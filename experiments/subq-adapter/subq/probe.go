package subq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProbeBody is the default check: the smallest valid request (one noul
// question, a one-word state). It is a single tiny inference request.
const ProbeBody = `{"state":"probe","questions":{"probe":{"type":"noul","instructions":"Is this a connectivity probe?"}}}`

// SmokeBody is the paid smoke request: one question of each type, in Jev's
// frozen format. Its response must parse and normalize.
const SmokeBody = `{"model":"jev-latest","state":"Shoes arrived two weeks late and in the wrong size. Also I see two charges on my card.","questions":{"department":{"type":"choice","instructions":"Which team should handle this?","criteria":{"returns":"Exchanges, refunds, wrong or damaged items","shipping":"Delivery status, delays, lost packages","billing":"Charges, invoices, payment problems"}},"escalate":{"type":"noul","instructions":"Does this need urgent human attention?"},"frustration":{"type":"score","instructions":"How frustrated is the customer?","criteria":["Calm","Frustrated","Very angry"]}}}`

// ValidationModel is the deliberately nonexistent model name used by the
// free validation check: the server validates the body before resolving the
// model, so a valid body gets 404 and an invalid one 422/400, and no inference
// can happen.
const ValidationModel = "subq-port-validation-nonexistent-model"

// FakeHeader marks responses from subq-fake.
const FakeHeader = "X-Subq-Fake"

// Probe statuses.
const (
	StatusConfigError    = "config_error"    // no model or no key
	StatusUnreachable    = "unreachable"     // transport error
	StatusNotAllowlisted = "not_allowlisted" // 404 model not found/inaccessible/not deployed
	StatusAuthFailed     = "auth_failed"     // 401/403
	StatusRejected       = "rejected"        // any other non-2xx
	StatusOpen           = "open"            // 2xx: the model answers
	StatusSmokeFailed    = "smoke_failed"
	StatusOK             = "ok" // open and the smoke request normalized
)

// ProbeResult is the outcome of subq-probe.
type ProbeResult struct {
	Checked    string         `json:"checked_utc"`
	Preview    string         `json:"preview"`
	Endpoint   string         `json:"endpoint"`
	Model      string         `json:"model"`
	Status     string         `json:"status"`
	Fake       bool           `json:"fake"`
	HTTPStatus int            `json:"http_status,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	RequestID  string         `json:"server_request_id,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	USD        float64        `json:"usd,omitempty"`
	Config     map[string]any `json:"config"`
	Smoke      map[string]any `json:"smoke,omitempty"`
	SmokeError string         `json:"smoke_error,omitempty"`
}

// Open reports whether the probe found the model answering.
func (r ProbeResult) Open() bool { return r.Status == StatusOpen || r.Status == StatusOK }

// Probe sends ProbeBody with the configured model and classifies the answer;
// with smoke it then sends SmokeBody and requires a response that parses
// (every answer typed, probabilities over every option, usage present).
func Probe(ctx context.Context, h *http.Client, c Config, smoke bool) ProbeResult {
	c = c.Resolve()
	r := ProbeResult{Checked: time.Now().UTC().Format(time.RFC3339), Preview: "public preview",
		Endpoint: c.URL, Model: c.Model, Config: c.Meta()}
	if e := c.RequireModel(); e != nil {
		r.Status, r.Detail = StatusConfigError, e.Error()
		return r
	}
	if c.NeedsKey() {
		r.Status, r.Detail = StatusConfigError, KeyMissing
		return r
	}
	cl := Client{HTTP: h, Config: c}
	res, e := cl.Do(ctx, []byte(ProbeBody))
	r.HTTPStatus, r.RequestID = res.Status, res.RequestID
	if e != nil {
		r.Status, r.Detail = StatusUnreachable, e.Error()
		return r
	}
	r.Fake = res.Fake
	switch {
	case NotAllowlisted(res.Status, res.Raw):
		r.Status, r.Detail = StatusNotAllowlisted, ErrorDetail(res.Raw)
		return r
	case res.Status == 401 || res.Status == 403:
		r.Status, r.Detail = StatusAuthFailed, ErrorDetail(res.Raw)
		return r
	case res.Status < 200 || res.Status >= 300:
		r.Status, r.Detail = StatusRejected, fmt.Sprintf("HTTP %d %s", res.Status, ErrorDetail(res.Raw))
		return r
	}
	u := res.Usage()
	r.Status, r.Usage, r.USD = StatusOpen, &u, c.USDOf(u)
	if res.ParseError != "" {
		r.Detail = "probe response did not normalize: " + res.ParseError
	}
	if !smoke {
		return r
	}
	res, e = cl.Do(ctx, []byte(SmokeBody))
	r.Smoke = map[string]any{"status": res.Status, "round_trip_seconds": res.Seconds, "sent": json.RawMessage(res.Sent),
		"server_request_id": res.RequestID, "server_processing_time": res.ServerTime}
	if json.Valid(res.Raw) {
		r.Smoke["response"] = json.RawMessage(res.Raw)
	}
	fail := func(msg string) ProbeResult { r.Status, r.SmokeError = StatusSmokeFailed, msg; return r }
	if e != nil {
		return fail(e.Error())
	}
	if !res.OK() {
		return fail(fmt.Sprintf("HTTP %d %s %s", res.Status, res.ParseError, ErrorDetail(res.Raw)))
	}
	su := res.Usage()
	r.Smoke["usage"], r.Smoke["usd"] = su, c.USDOf(su)
	if su.InputTokens <= 0 {
		return fail("usage.input_tokens is not positive")
	}
	if r.Detail != "" {
		return fail(r.Detail)
	}
	r.Status = StatusOK
	return r
}

// Sample is one body chosen for live validation.
type Sample struct {
	Path  string `json:"path"`
	Shape string `json:"shape"`
	Bytes int    `json:"bytes"`
	Why   string `json:"why"`
	body  []byte
}

// SelectSamples reads every *.json body under the given files or
// directories and picks at most max of them: the largest body of each
// distinct Shape first (shapes with non-standard fields first), then the
// largest bodies overall.
func SelectSamples(paths []string, max int) ([]Sample, error) {
	var all []Sample
	for _, p := range paths {
		e := filepath.WalkDir(p, func(f string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() || !strings.HasSuffix(f, ".json") {
				return nil
			}
			b, e := os.ReadFile(f)
			if e != nil {
				return e
			}
			b = bytes.TrimSpace(b)
			if !json.Valid(b) || !bytes.Contains(b, []byte(`"questions"`)) {
				return nil
			}
			all = append(all, Sample{Path: f, Shape: Shape(b), Bytes: len(b), body: b})
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Bytes > all[j].Bytes })
	var out []Sample
	picked := map[string]bool{}
	shapes := map[string]bool{}
	var order []string
	for _, s := range all {
		if !shapes[s.Shape] {
			shapes[s.Shape] = true
			order = append(order, s.Shape)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return strings.Contains(order[i], "+") && !strings.Contains(order[j], "+") })
	for _, sh := range order {
		for _, s := range all {
			if s.Shape == sh && len(out) < max {
				s.Why = "largest of shape"
				out = append(out, s)
				picked[s.Path] = true
				break
			}
		}
	}
	for _, s := range all {
		if len(out) >= max {
			break
		}
		if !picked[s.Path] {
			s.Why = "largest overall"
			out = append(out, s)
			picked[s.Path] = true
		}
	}
	return out, nil
}

// LiveCheck is one live validation result.
type LiveCheck struct {
	Sample
	OfflineError string `json:"offline_error,omitempty"`
	Edits        []Edit `json:"prepare_edits,omitempty"`
	SentBytes    int    `json:"sent_bytes"`
	HTTPStatus   int    `json:"http_status"`
	Verdict      string `json:"verdict"` // valid (404 model), invalid (400/413/422), unexpected
	Detail       string `json:"detail,omitempty"`
	RequestID    string `json:"server_request_id,omitempty"`
}

// ValidateLive sends each sample, prepared exactly as a real run would
// prepare it but with model = ValidationModel, sequentially, and classifies
// the server's answer. No inference can happen: the model does not exist, and
// the server validates bodies before resolving the model. A body that fails
// offline validation is still sent (with only the model replaced) so the
// server's verdict is recorded too.
func ValidateLive(ctx context.Context, h *http.Client, c Config, samples []Sample, pause time.Duration) []LiveCheck {
	c = c.Resolve()
	c.Model = ValidationModel
	cl := Client{HTTP: h, Config: c}
	var out []LiveCheck
	for i, s := range samples {
		if i > 0 && pause > 0 {
			time.Sleep(pause)
		}
		lc := LiveCheck{Sample: s}
		sent, edits, e := c.PrepareEdits(s.body)
		if e != nil {
			lc.OfflineError = e.Error()
			sent, edits = c.setModelOnly(s.body)
		}
		lc.Edits, lc.SentBytes = edits, len(sent)
		res, e := cl.DoRaw(ctx, sent)
		lc.HTTPStatus, lc.RequestID = res.Status, res.RequestID
		switch {
		case e != nil:
			lc.Verdict, lc.Detail = "unexpected", e.Error()
		case NotAllowlisted(res.Status, res.Raw):
			lc.Verdict = "valid"
		case res.Status == 400 || res.Status == 413 || res.Status == 422:
			lc.Verdict, lc.Detail = "invalid", ErrorDetail(res.Raw)
		default:
			lc.Verdict, lc.Detail = "unexpected", fmt.Sprintf("HTTP %d %.300s", res.Status, res.Raw)
		}
		out = append(out, lc)
		if res.Status >= 200 && res.Status < 300 {
			break // the validation model must never answer; stop immediately
		}
	}
	return out
}

// setModelOnly replaces or inserts model without any other change, for
// bodies that fail offline validation.
func (c Config) setModelOnly(frozen []byte) ([]byte, []Edit) {
	ms, e := objectMembers(frozen)
	if e != nil {
		return frozen, nil
	}
	model, _ := json.Marshal(c.Model)
	var buf bytes.Buffer
	buf.WriteString(`{"model":`)
	buf.Write(model)
	for _, m := range ms {
		if m.key == "model" {
			continue
		}
		kb, _ := json.Marshal(m.key)
		buf.WriteByte(',')
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(m.val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), []Edit{{Op: "set_model", Field: "model"}}
}
