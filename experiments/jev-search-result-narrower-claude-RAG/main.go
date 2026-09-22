package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type M = map[string]any

const fw = "https://api.fireworks.ai/inference/v1"
const jevURL = "https://api.typesafe.ai/v1/systemone"
const revision = "c0b3a9190fd970e83cfbe7d399a08860e43e221e"

type Config struct {
	FilterMode        string  `json:"filter_mode,omitempty"`
	ExcludedQuestions string  `json:"excluded_questions"`
	ClaudeCommand     string  `json:"claude_command"`
	Questions         int     `json:"questions"`
	Seed              int     `json:"seed"`
	DataDir           string  `json:"data_dir"`
	DenseNS           string  `json:"dense_namespace"`
	LexicalNS         string  `json:"lexical_namespace"`
	Region            string  `json:"region"`
	QA                string  `json:"qa_model"`
	DeepSeek          string  `json:"deepseek_model"`
	Jev               string  `json:"jev_model"`
	Embedding         string  `json:"embedding_model"`
	Reranker          string  `json:"reranker_model"`
	RetrievalK        int     `json:"retrieval_k"`
	Candidates        int     `json:"rerank_candidates"`
	ResultK           int     `json:"result_k"`
	MaxSearches       int     `json:"max_searches"`
	Threshold         float64 `json:"filter_threshold"`
	JevPageTokens     int     `json:"jev_page_proxy_tokens"`
	JevPageBytes      int     `json:"jev_page_bytes"`
	JevMaxQuestions   int     `json:"jev_max_questions"`
	QAMaxTokens       int     `json:"qa_max_tokens"`
	Workers           int     `json:"workers"`
	MaxRequests       int     `json:"max_requests"`
	Timeout           int     `json:"timeout_seconds"`
}
type Question struct {
	ID       string `json:"question_id"`
	Question string `json:"question"`
	DocID    string `json:"document_id"`
}
type Unit struct {
	ID    string `json:"id"`
	DocID string `json:"document_id"`
	Text  string `json:"text"`
	Start int    `json:"start_byte"`
	End   int    `json:"end_byte"`
}
type Store struct {
	Dir  string
	mu   sync.Mutex
	rows map[string]map[string]M
}

func canon(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func str(v any) string     { s, _ := v.(string); return s }
func num(v any) float64    { n, _ := v.(float64); return n }
func obj(v any) M {
	m, _ := v.(map[string]any)
	if m == nil {
		return M{}
	}
	return m
}
func arr(v any) []any { a, _ := v.([]any); return a }
func scan(path string, fn func([]byte) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 128<<20)
	for s.Scan() {
		if e = fn(s.Bytes()); e != nil {
			return e
		}
	}
	return s.Err()
}
func jsonFile(path string, v any) error {
	b := append(canon(v), '\n')
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func newStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := &Store{Dir: dir, rows: map[string]map[string]M{}}
	return s, nil
}
func (s *Store) loadLocked(name string) error {
	if s.rows[name] != nil {
		return nil
	}
	s.rows[name] = map[string]M{}
	e := scan(filepath.Join(s.Dir, name+".jsonl"), func(b []byte) error {
		var r M
		if e := json.Unmarshal(b, &r); e != nil {
			return fmt.Errorf("invalid JSONL %s; preserve/repair torn tail before resuming: %w", name, e)
		}
		id := str(r["id"])
		if id == "" {
			return fmt.Errorf("missing record ID in %s", name)
		}
		if _, ok := s.rows[name][id]; ok {
			return fmt.Errorf("duplicate %s/%s", name, id)
		}
		s.rows[name][id] = r
		return nil
	})
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	return e
}
func (s *Store) get(name, id string) (M, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.loadLocked(name); e != nil {
		panic(e)
	}
	r, ok := s.rows[name][id]
	return r, ok
}
func (s *Store) all(name string) []M {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.loadLocked(name); e != nil {
		panic(e)
	}
	a := []M{}
	for _, r := range s.rows[name] {
		a = append(a, r)
	}
	sort.Slice(a, func(i, j int) bool { return str(a[i]["id"]) < str(a[j]["id"]) })
	return a
}
func (s *Store) put(name string, r M) error {
	// Normalize to the exact JSON types used after a restart; detach caller-owned maps.
	var normalized M
	if e := json.Unmarshal(canon(r), &normalized); e != nil {
		return e
	}
	r = normalized
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.loadLocked(name); e != nil {
		return e
	}
	id := str(r["id"])
	if id == "" {
		return fmt.Errorf("missing id")
	}
	if old, ok := s.rows[name][id]; ok {
		if !bytes.Equal(canon(old), canon(r)) {
			return fmt.Errorf("refusing changed record %s/%s", name, id)
		}
		return nil
	}
	f, e := os.OpenFile(filepath.Join(s.Dir, name+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(canon(r), '\n'))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		s.rows[name][id] = r
	}
	return e
}

type Harness struct {
	C           Config
	S           *Store
	Ctx         context.Context
	HTTP        *http.Client
	Units       map[string]Unit
	reqMu       sync.Mutex
	requests    int
	searchLocks sync.Map
}

func (h *Harness) call(key, phase, url, credential string, payload any) (M, error) {
	identity := hash(canon(M{"url": url, "payload": payload}))
	cacheID := key + ":" + identity
	if r, ok := h.S.get("responses", cacheID); ok {
		return obj(r["response"]), nil
	}
	token := os.Getenv(credential)
	if token == "" {
		return nil, fmt.Errorf("missing %s", credential)
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		h.reqMu.Lock()
		if h.requests >= h.C.MaxRequests {
			h.reqMu.Unlock()
			return nil, fmt.Errorf("request ceiling reached")
		}
		h.requests++
		seq := h.requests
		h.reqMu.Unlock()
		id := fmt.Sprintf("request-%06d", seq)
		start := time.Now()
		reservation := M{"id": id, "key": key, "phase": phase, "url": url, "payload_sha256": identity, "started_utc": start.UTC().Format(time.RFC3339Nano)}
		if e := h.S.put("reservations", reservation); e != nil {
			return nil, e
		}
		method := "POST"
		var body io.Reader = bytes.NewReader(canon(payload))
		if payload == nil {
			method = "GET"
			body = nil
		}
		req, e := http.NewRequestWithContext(h.Ctx, method, url, body)
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, e := h.HTTP.Do(req)
		status := 0
		var raw []byte
		if e == nil {
			status = resp.StatusCode
			raw, e = io.ReadAll(io.LimitReader(resp.Body, 128<<20))
			resp.Body.Close()
		}
		errorText := ""
		if e != nil {
			errorText = "request transport failed"
		} else if status < 200 || status >= 300 {
			errorText = fmt.Sprintf("HTTP %d", status)
		}
		var data M
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &data)
		}
		record := M{"id": id, "key": key, "phase": phase, "url": url, "payload_sha256": identity, "payload": payload, "response": data, "status": status, "error": errorText, "elapsed_s": time.Since(start).Seconds(), "started_utc": start.UTC().Format(time.RFC3339Nano)}
		if data == nil && len(raw) > 0 {
			record["raw_response"] = string(raw)
		}
		if err := h.S.put("requests", record); err != nil {
			return nil, err
		}
		if errorText == "" && data != nil {
			if e = h.S.put("responses", M{"id": cacheID, "request_id": id, "response": data}); e != nil {
				return nil, e
			}
			return data, nil
		}
		last = fmt.Errorf("%s: %s", key, errorText)
		if errorText == "" {
			last = fmt.Errorf("%s: invalid JSON response", key)
		}
		if status != 0 && status != 408 && status != 409 && status != 429 && status < 500 {
			return nil, last
		}
		select {
		case <-h.Ctx.Done():
			return nil, h.Ctx.Err()
		case <-time.After(time.Duration(2<<attempt) * time.Second):
		}
	}
	return nil, last
}
func (h *Harness) tp(key, phase, ns, path string, payload any) (M, error) {
	return h.call(key, phase, "https://"+h.C.Region+".turbopuffer.com/"+path+ns, "TURBOPUFFER_API_KEY", payload)
}
func (h *Harness) cli(args ...string) ([]byte, error) {
	base := []string{"run", "--project", "EnronQA-cli", "--frozen", "enronqa"}
	cmd := exec.CommandContext(h.Ctx, "uv", append(base, args...)...)
	var errout bytes.Buffer
	cmd.Stderr = &errout
	b, e := cmd.Output()
	if e != nil {
		return nil, fmt.Errorf("EnronQA-cli %v: %w: %s", args, e, errout.String())
	}
	return b, nil
}
func (h *Harness) questions() ([]Question, error) {
	qs := []Question{}
	seen := map[string]bool{}
	e := scan("data/questions.jsonl", func(b []byte) error {
		var q Question
		if e := json.Unmarshal(b, &q); e != nil {
			return e
		}
		if q.ID == "" || q.Question == "" || seen[q.ID] {
			return fmt.Errorf("invalid/duplicate question")
		}
		seen[q.ID] = true
		qs = append(qs, q)
		return nil
	})
	if e == nil && len(qs) != h.C.Questions {
		e = fmt.Errorf("expected %d questions, got %d", h.C.Questions, len(qs))
	}
	return qs, e
}
func (h *Harness) parallel(ids []string, fn func(string) error) error {
	ctx, cancel := context.WithCancel(h.Ctx)
	defer cancel()
	jobs := make(chan string)
	var wg sync.WaitGroup
	var first error
	var once sync.Once
	for i := 0; i < h.C.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if e := fn(id); e != nil {
					once.Do(func() { first = e; cancel() })
				}
			}
		}()
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	return first
}
func (h *Harness) freeze() error {
	b, e := os.ReadFile("data/questions.jsonl")
	if e != nil {
		return e
	}
	submodule, e := exec.Command("git", "-C", "EnronQA-cli", "rev-parse", "HEAD").Output()
	if e != nil {
		return e
	}
	corpus, e := os.ReadFile("data/units.jsonl")
	if e != nil {
		return e
	}
	meta := M{"id": "protocol", "enronqa_cli_commit": string(bytes.TrimSpace(submodule)), "corpus_sha256": hash(corpus), "config": h.C, "questions_sha256": hash(b), "dataset_revision": revision, "prompts_sha256": hash([]byte(answerPrompt + filterPrompt + judgePrompt + claudeProtocol)), "source_sha256": sourceHash()}
	excluded, e := os.ReadFile(h.C.ExcludedQuestions)
	if e != nil {
		return e
	}
	meta["excluded_questions_sha256"] = hash(excluded)
	return h.S.put("manifest", meta)
}
func sourceHash() string {
	paths, _ := filepath.Glob("*.go")
	paths = append(paths, "go.mod", "go.sum")
	sort.Strings(paths)
	m := M{}
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			panic(e)
		}
		m[p] = hash(b)
	}
	return hash(canon(m))
}
func main() {
	if e := mainErr(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func mainErr() error {
	var config, run string
	root := &cobra.Command{Use: "jev-search-result-narrower-claude-RAG", Short: "Paired EnronQA hybrid-search / Jev-filter experiment", SilenceUsage: true}
	root.PersistentFlags().StringVar(&config, "config", "config.json", "Frozen experiment configuration")
	root.PersistentFlags().StringVar(&run, "run", "runs/main", "JSONL evidence directory")
	addAnalysisCommands(root, &run)
	addRescoreCommand(root, &run)
	addImportBaselineCommand(root)

	for _, name := range []string{"prepare", "preflight", "run", "report", "status"} {
		name := name
		root.AddCommand(&cobra.Command{Use: name, RunE: func(cmd *cobra.Command, args []string) error {
			var c Config
			b, e := os.ReadFile(config)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(b, &c); e != nil {
				return e
			}
			if c.FilterMode != "" && c.FilterMode != "probability" && c.FilterMode != "categorical" {
				return fmt.Errorf("unknown filter mode")
			}
			if c.Questions < 1 || c.Workers < 1 || c.ResultK < 1 || c.MaxSearches < 1 || c.Threshold < 0 || c.Threshold > 1 {
				return fmt.Errorf("invalid configuration")
			}
			home, _ := os.UserHomeDir()
			_ = godotenv.Load(".env", filepath.Join(home, ".secrets/keys.env"))
			s, e := newStore(run)
			if e != nil {
				return e
			}
			lock, e := os.OpenFile(filepath.Join(run, "run.lock"), os.O_CREATE|os.O_RDWR, 0600)
			if e != nil {
				return e
			}
			defer lock.Close()
			if name != "status" {
				if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
					return fmt.Errorf("another writer is active")
				}
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			h := &Harness{C: c, S: s, Ctx: ctx, HTTP: &http.Client{Timeout: time.Duration(c.Timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			h.requests = len(s.all("reservations"))
			switch name {
			case "prepare":
				return h.prepare()
			case "preflight":
				return h.preflight()
			case "run":
				if e = h.prepareSample(); e != nil {
					return e
				}
				if e = h.preflight(); e != nil {
					return e
				}
				if e = h.freeze(); e != nil {
					return e
				}
				if e = h.loadUnits(); e != nil {
					return e
				}
				if e = h.runAnswers(); e != nil {
					return e
				}
				if e = h.runJudges(); e != nil {
					return e
				}
				return h.report()
			case "report":
				return h.report()
			case "status":
				fmt.Println(string(canon(M{"answers": len(s.all("answers")), "judgments": len(s.all("judgments")), "searches": len(s.all("searches")), "filters": len(s.all("filters")), "requests": len(s.all("requests"))})))
				return nil
			}
			return nil
		}})
	}
	return root.Execute()
}
