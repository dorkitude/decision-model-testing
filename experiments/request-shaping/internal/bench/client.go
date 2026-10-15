package bench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const Endpoint = "https://api.typesafe.ai/v1/systemone"
const PricePerToken = 0.042 / 1e6

// MaxAttempts covers transport, 408, 429 and 5xx failures; every attempt is kept.
const MaxAttempts = 4

type Receipt struct {
	ID         string          `json:"id"`
	Arm        string          `json:"arm"`
	Attempt    int             `json:"attempt"`
	URL        string          `json:"url"`
	PayloadSHA string          `json:"payload_sha256"`
	Bytes      int             `json:"payload_bytes"`
	Started    string          `json:"started_utc"`
	Seconds    float64         `json:"seconds"`
	Status     int             `json:"status"`
	Response   json.RawMessage `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
	UpperUSD   float64         `json:"reserved_upper_usd"`

	// Set only when --jev-model replaces Model on the wire.
	ProviderModel string `json:"provider_model,omitempty"`
	WireSHA       string `json:"wire_request_sha256,omitempty"`
	WireBytes     int    `json:"wire_request_bytes,omitempty"`
}

func (r Receipt) OK() bool { return r.Error == "" && r.Status >= 200 && r.Status < 300 }

func transient(status int) bool {
	return status == 0 || status == 408 || status == 429 || status >= 500
}

// OverrideMaxAttempts is the attempt limit of a --jev-model run.
const OverrideMaxAttempts = 8

func attempts() int {
	if Override() {
		return OverrideMaxAttempts
	}
	return MaxAttempts
}

// retryable is transient, plus, on a --jev-model run, HTTP 499 with
// error_type "internal_error" (a server-side failure).
func retryable(rc Receipt) bool {
	return transient(rc.Status) || Override() && rc.Status == 499 && bytes.Contains(rc.Response, []byte(`"internal_error"`))
}

// checkModel keeps receipts of different Jev models out of one directory.
func checkModel(rc Receipt, path string) error {
	want := ""
	if Override() {
		want = WireModel
	}
	if rc.ProviderModel != want {
		return fmt.Errorf("receipt %s is from Jev model %q, not %q; use a new output directory", path, rc.ProviderModel, WireModel)
	}
	return nil
}

// UpperCost bounds billable input tokens by UTF-8 bytes plus fixed overhead.
func UpperCost(body []byte) float64 { return float64(len(body)+1024) * PricePerToken }

type Runner struct {
	Out     string
	Budget  float64
	Workers int
	RPM     float64
	Guard   int // maximum request bytes

	http     *http.Client
	token    string
	mu       sync.Mutex
	reserved float64
	next     time.Time
	calls    atomic.Int64
	failures atomic.Int64
}

func (r *Runner) receiptPath(id string, attempt int) string {
	return filepath.Join(r.Out, "receipts", id+"-"+strconv.Itoa(attempt)+".json")
}

func Token() (string, error) {
	if t := os.Getenv("TYPESAFE_TOKEN"); t != "" {
		return t, nil
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(filepath.Join(home, ".secrets/keys.env"))
	if e != nil {
		return "", e
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != "TYPESAFE_TOKEN" {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("missing credential TYPESAFE_TOKEN")
}

func Lock(out string) (func(), error) {
	if e := os.MkdirAll(out, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(out, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("another runner owns %s", out)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func (r *Runner) wait() {
	r.mu.Lock()
	now := time.Now()
	if r.next.Before(now) {
		r.next = now
	}
	at := r.next
	r.next = r.next.Add(time.Duration(float64(time.Minute) / r.RPM))
	r.mu.Unlock()
	time.Sleep(time.Until(at))
}

func (r *Runner) pause(d time.Duration) {
	r.mu.Lock()
	if t := time.Now().Add(d); t.After(r.next) {
		r.next = t
	}
	r.mu.Unlock()
}

// Run executes jobs in the given order, resuming from saved receipts.
func (r *Runner) Run(jobs []Job) error {
	unlock, e := Lock(r.Out)
	if e != nil {
		return e
	}
	defer unlock()
	if left, _ := filepath.Glob(filepath.Join(r.Out, "reservations", "*.json")); len(left) > 0 {
		return fmt.Errorf("%d unresolved reservations in %s; a request may have reached the provider without a receipt; reconcile before resuming", len(left), r.Out)
	}
	files, _ := filepath.Glob(filepath.Join(r.Out, "receipts", "*.json"))
	for _, f := range files {
		var rc Receipt
		if e := ReadJSON(f, &rc); e != nil {
			return e
		}
		r.reserved += rc.UpperUSD
	}
	if r.token, e = Token(); e != nil {
		return e
	}
	r.http = &http.Client{Timeout: 120 * time.Second}
	ch := make(chan Job)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	var done atomic.Int64
	start := time.Now()
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				fmt.Fprintf(os.Stderr, "%s  %d/%d jobs  %d calls  reserved $%.4f\n", time.Since(start).Round(time.Second), done.Load(), len(jobs), r.calls.Load(), r.reserved)
			}
		}
	}()
	for w := 0; w < r.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if _, e := r.call(j); e != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", j.ID, e)
					}
					errMu.Unlock()
				}
				done.Add(1)
			}
		}()
	}
	for _, j := range jobs {
		errMu.Lock()
		failed := firstErr != nil
		errMu.Unlock()
		if failed {
			break
		}
		ch <- j
	}
	close(ch)
	wg.Wait()
	close(stop)
	fmt.Fprintf(os.Stderr, "finished %d/%d jobs, %d new calls in %s\n", done.Load(), len(jobs), r.calls.Load(), time.Since(start).Round(time.Second))
	return firstErr
}

var ErrExhausted = errors.New("transient attempts exhausted")

func (r *Runner) call(j Job) (Receipt, error) {
	h := Digest(j.Body)
	if r.Guard > 0 && len(j.Body) > r.Guard {
		return Receipt{}, fmt.Errorf("request is %d bytes, over the %d-byte guard", len(j.Body), r.Guard)
	}
	sent, e := WireBody(j.Body)
	if e != nil {
		return Receipt{}, e
	}
	for attempt := 1; attempt <= attempts(); attempt++ {
		path := r.receiptPath(j.ID, attempt)
		var rc Receipt
		if e := ReadJSON(path, &rc); e == nil {
			if rc.PayloadSHA != h {
				return rc, fmt.Errorf("receipt payload mismatch %s", path)
			}
			if e := checkModel(rc, path); e != nil {
				return rc, e
			}
			if rc.OK() {
				if _, _, e := ParseNoulsAs(rc.Response, j.Keys(), WireModel); e != nil {
					return rc, fmt.Errorf("cached malformed answer: %w", e)
				}
				return rc, nil
			}
			if !retryable(rc) {
				return rc, fmt.Errorf("cached permanent failure: %s", rc.Error)
			}
			continue
		} else if !os.IsNotExist(e) {
			return rc, e
		}
		upper := UpperCost(j.Body)
		r.mu.Lock()
		if r.reserved+upper > r.Budget {
			r.mu.Unlock()
			return rc, fmt.Errorf("conservative budget ceiling $%.2f reached", r.Budget)
		}
		r.reserved += upper
		r.mu.Unlock()
		rc = Receipt{ID: j.ID, Arm: j.Arm, Attempt: attempt, URL: Endpoint, PayloadSHA: h, Bytes: len(j.Body), UpperUSD: upper}
		if Override() {
			rc.ProviderModel, rc.WireSHA, rc.WireBytes = WireModel, Digest(sent), len(sent)
		}
		reservation := filepath.Join(r.Out, "reservations", j.ID+"-"+strconv.Itoa(attempt)+".json")
		if e := WriteJSON(reservation, rc); e != nil {
			return rc, e
		}
		r.wait()
		rc.Started = time.Now().UTC().Format(time.RFC3339Nano)
		req, e := http.NewRequest("POST", Endpoint, bytes.NewReader(sent))
		if e != nil {
			return rc, e
		}
		req.Header.Set("Authorization", "Bearer "+r.token)
		req.Header.Set("Content-Type", "application/json")
		t0 := time.Now()
		resp, e := r.http.Do(req)
		r.calls.Add(1)
		var raw []byte
		retryAfter := 0
		if e == nil {
			rc.Status = resp.StatusCode
			retryAfter, _ = strconv.Atoi(resp.Header.Get("Retry-After"))
			raw, e = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			resp.Body.Close()
		}
		rc.Seconds = time.Since(t0).Seconds()
		switch {
		case e != nil:
			rc.Error = "transport error"
		case !rc.OK():
			rc.Error = fmt.Sprintf("HTTP %d", rc.Status)
		}
		if json.Valid(raw) {
			rc.Response = raw
		} else if len(raw) > 0 {
			rc.Response = Encode(M{"non_json_body_sha256": Digest(raw), "bytes": len(raw)})
			if rc.Error == "" {
				rc.Error = "invalid JSON"
			}
		}
		// Credentials and headers are never persisted; responses contain answers and usage only.
		if e := WriteJSON(path, rc); e != nil {
			return rc, e
		}
		os.Remove(reservation)
		if rc.OK() {
			if _, _, e := ParseNoulsAs(rc.Response, j.Keys(), WireModel); e != nil {
				return rc, fmt.Errorf("malformed answer: %w", e)
			}
			return rc, nil
		}
		r.failures.Add(1)
		if !retryable(rc) {
			return rc, fmt.Errorf("permanent failure: %s %s", rc.Error, truncate(string(raw), 300))
		}
		backoff := time.Duration(1<<attempt) * time.Second
		if retryAfter > 0 {
			backoff = time.Duration(retryAfter) * time.Second
		}
		if rc.Status == 429 {
			r.pause(backoff)
		}
		time.Sleep(backoff)
	}
	return Receipt{}, ErrExhausted
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Answer is the replayed outcome of one job.
type Answer struct {
	Job      Job
	Nouls    map[string]float64
	Tokens   int
	Seconds  float64 // successful attempt
	Attempts int
	Started  time.Time
	OK       bool
}

// Replay reconstructs answers from receipts without network access.
func Replay(out string, jobs []Job) ([]Answer, error) {
	var res []Answer
	r := Runner{Out: out}
	for _, j := range jobs {
		h := Digest(j.Body)
		a := Answer{Job: j}
		for attempt := 1; attempt <= attempts(); attempt++ {
			var rc Receipt
			path := r.receiptPath(j.ID, attempt)
			if e := ReadJSON(path, &rc); e != nil {
				if os.IsNotExist(e) {
					break
				}
				return nil, e
			}
			if rc.PayloadSHA != h {
				return nil, fmt.Errorf("payload mismatch for %s", j.ID)
			}
			if e := checkModel(rc, path); e != nil {
				return nil, e
			}
			a.Attempts = attempt
			if rc.OK() {
				n, tok, e := ParseNoulsAs(rc.Response, j.Keys(), WireModel)
				if e != nil {
					return nil, fmt.Errorf("%s: %w", j.ID, e)
				}
				a.Nouls, a.Tokens, a.Seconds, a.OK = n, tok, rc.Seconds, true
				a.Started, _ = time.Parse(time.RFC3339Nano, rc.Started)
				break
			}
		}
		res = append(res, a)
	}
	return res, nil
}

// Interleave orders jobs with a seeded shuffle so arms share the same time window.
func Interleave(jobs []Job, seed uint64) []Job {
	out := Shuffled(jobs, RNG(seed, "schedule"))
	return out
}

// Manifest records the plan identity so a changed plan cannot silently reuse a run directory.
func Manifest(out, study string, jobs []Job, params M) error {
	h := sha256Jobs(jobs)
	path := filepath.Join(out, "manifest.json")
	var old M
	if e := ReadJSON(path, &old); e == nil {
		if old["plan_sha256"] != h {
			return fmt.Errorf("plan changed since %s was created; use a new output directory", out)
		}
		if m, _ := old["model"].(string); m != WireModel {
			return fmt.Errorf("%s was created for %s, not %s; use a new output directory", out, m, WireModel)
		}
		return nil
	}
	arms := map[string]int{}
	decisions := map[string]int{}
	for _, j := range jobs {
		arms[j.Arm]++
		decisions[j.Arm] += len(j.Decisions)
	}
	m := M{"study": study, "model": WireModel, "dataset_revision": DatasetRevision, "sources": Sources, "plan_sha256": h, "jobs": len(jobs), "requests_by_arm": arms, "decisions_by_arm": decisions, "params": params, "created_utc": time.Now().UTC().Format(time.RFC3339)}
	if Override() {
		m["request_model"], m["max_attempts"] = Model, OverrideMaxAttempts
	}
	return WriteJSON(path, m)
}

func sha256Jobs(jobs []Job) string {
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID + " " + Digest(j.Body)
	}
	sort.Strings(ids)
	return Digest([]byte(strings.Join(ids, "\n")))
}
