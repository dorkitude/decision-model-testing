package decisions

// Wire format for POST /v1/decisions.
//
// STATUS: DOCUMENTED. Follows the public-beta guide published 2026-10-06
// (https://developers.openai.com/api/docs/guides/decisions), checked against
// validation errors and a live response. It replaced the assumed format
// ("assumed-2026-09-29") before any benchmark ran. WireVersion is recorded in
// every receipt. Parsing is strict: every question must be answered exactly
// once, with one of its own options and probabilities in [0, 1].

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// Endpoint is the default: the OpenAI API (needs a key from TokenEnv). The
	// published runs went through an exe.dev `openai` integration, an
	// https://<name>.int.exe.xyz proxy that injects the key at the network
	// edge; IsIntegration recognizes such URLs and TokenFor sends no key.
	Endpoint = "https://api.openai.com/v1/decisions"
	// DirectEndpoint is the OpenAI API itself, for machines without the
	// integration. Only this endpoint (or any non-integration URL, such as
	// decisions-fake) needs a credential from TokenEnv.
	DirectEndpoint = "https://api.openai.com/v1/decisions"
	DefaultModel   = "gpt-6-luna"
	// USDPerMTokInput is the published /v1/decisions price for gpt-6-luna:
	// input tokens only, no output or cache charges (guide, 2026-10-06).
	USDPerMTokInput = 0.10
	WireVersion     = "guide-2026-10-06"
)

// Official reports whether url is the real Decisions API, through the exe.dev
// integration or directly.
func Official(url string) bool { return url == Endpoint || url == DirectEndpoint }

// LogicalEndpoint is the endpoint identity used by receipts, caches and
// same-run checks: the integration URL maps to the OpenAI endpoint it fronts,
// so a run started on one route resumes (and replays) on the other. Frozen
// receipts recorded DirectEndpoint, which maps to itself.
func LogicalEndpoint(url string) string {
	if url == Endpoint {
		return DirectEndpoint
	}
	return url
}

// SameEndpoint reports whether two endpoint URLs are the same logical endpoint.
func SameEndpoint(a, b string) bool { return LogicalEndpoint(a) == LogicalEndpoint(b) }

// IsIntegration reports whether url goes through an exe.dev integration
// (https://<name>.int.exe.xyz), where the key is injected at the edge.
func IsIntegration(url string) bool {
	rest, ok := strings.CutPrefix(url, "https://")
	if !ok {
		return false
	}
	host, _, _ := strings.Cut(rest, "/")
	return strings.HasSuffix(host, ".int.exe.xyz")
}

// TokenFor returns the credential url needs: none for an exe.dev
// integration, Token() otherwise.
func TokenFor(url string) (string, error) {
	if url == "" {
		url = Endpoint
	}
	if IsIntegration(url) {
		return "", nil
	}
	return Token()
}

// Request: {model, input, questions:[{type, name, instructions, choices|levels}]}.
// Response: {model, answers:[{type, name, ...}], usage:{input_tokens, ...}}.
// Answer types: predicate {probability}; choice {choice, probabilities:
// [{value, probability}], confidence}; score {score, probabilities:[{value
// (level index), label, probability}], confidence}; refusal (no fields).

type wireChoice struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type wireLevel struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type wireQuestion struct {
	Type         string       `json:"type"`
	Name         string       `json:"name"`
	Instructions string       `json:"instructions"`
	Choices      []wireChoice `json:"choices,omitempty"`
	Levels       []wireLevel  `json:"levels,omitempty"`
}

type wireRequest struct {
	Model     string         `json:"model"`
	Input     string         `json:"input"`
	Questions []wireQuestion `json:"questions"`
}

// Encode renders a Plan as a Decisions API request body. Jev noul questions
// become predicates; Decisions predicates take no criteria, so optional
// true/false criteria are appended to the instructions (PredicateInstructions).
func Encode(p Plan, model string) ([]byte, error) {
	r := wireRequest{Model: model, Input: p.Context}
	for _, q := range p.Questions {
		w := wireQuestion{Name: q.Key, Instructions: q.Instructions}
		switch q.JevType {
		case "noul":
			w.Type, w.Instructions = "predicate", PredicateInstructions(q)
		case "choice":
			w.Type = "choice"
			for _, o := range q.Options {
				w.Choices = append(w.Choices, wireChoice(o))
			}
		case "score":
			w.Type = "score"
			for _, o := range q.Options {
				w.Levels = append(w.Levels, wireLevel{Label: o.Value, Description: o.Description})
			}
		default:
			return nil, fmt.Errorf("question %q has unsupported type %q", q.Key, q.JevType)
		}
		r.Questions = append(r.Questions, w)
	}
	return json.Marshal(r)
}

// PredicateInstructions is the predicate instruction text for a noul
// question: its instructions, plus any true/false criteria spelled out.
func PredicateInstructions(q Question) string {
	var t, f string
	for _, o := range q.Options {
		switch o.Value {
		case Yes:
			t = o.Description
		case No:
			f = o.Description
		}
	}
	s := q.Instructions
	if t != "" {
		s += "\n\nCount as true: " + t
	}
	if f != "" {
		s += "\n\nCount as false: " + f
	}
	return s
}

// Translate turns a frozen Jev body into a Decisions API body.
func Translate(jevBody []byte, model string) ([]byte, Plan, error) {
	p, e := ParsePlan(jevBody)
	if e != nil {
		return nil, Plan{}, e
	}
	b, e := Encode(p, model)
	return b, p, e
}

type wireProb struct {
	Value       json.RawMessage `json:"value"` // string (choice) or level index (score)
	Label       string          `json:"label"`
	Probability *float64        `json:"probability"`
}

type wireAnswer struct {
	Type          string     `json:"type"`
	Name          string     `json:"name"`
	Probability   *float64   `json:"probability"`
	Choice        *string    `json:"choice"`
	Score         *float64   `json:"score"`
	Probabilities []wireProb `json:"probabilities"`
	Confidence    *float64   `json:"confidence"`
}

// Decode reads a Decisions API response into provider-neutral answers.
func Decode(raw []byte) (model string, answers []Answer, u Usage, err error) {
	var r struct {
		Model   string          `json:"model"`
		Answers []wireAnswer    `json:"answers"`
		Usage   json.RawMessage `json:"usage"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return
	}
	if r.Answers == nil {
		err = fmt.Errorf("response has no answers array")
		return
	}
	model = r.Model
	if len(r.Usage) > 0 && string(r.Usage) != "null" {
		var x struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		}
		if err = json.Unmarshal(r.Usage, &x); err != nil {
			return
		}
		u = Usage{InputTokens: x.In, OutputTokens: x.Out, Reported: true}
	}
	for _, w := range r.Answers {
		a := Answer{Key: w.Name, Type: w.Type, Confidence: w.Confidence, Probability: w.Probability, Score: w.Score}
		if w.Choice != nil {
			a.Value = *w.Choice
		}
		if len(w.Probabilities) > 0 {
			a.Scores = map[string]float64{}
			for _, pr := range w.Probabilities {
				if pr.Probability == nil {
					err = fmt.Errorf("answer %q has a probability entry without a probability", w.Name)
					return
				}
				var k string
				if json.Unmarshal(pr.Value, &k) != nil {
					// Score levels are keyed by their index; Scores uses the label.
					var i int
					if json.Unmarshal(pr.Value, &i) != nil {
						err = fmt.Errorf("answer %q has an unreadable probability value", w.Name)
						return
					}
					k = strconv.Itoa(i)
					a.LevelLabels = append(a.LevelLabels, pr.Label)
				}
				a.Scores[k] = *pr.Probability
			}
		}
		answers = append(answers, a)
	}
	return
}

// Normalize converts a raw Decisions response into a Jev-shaped response.
func Normalize(p Plan, requestedModel string, raw []byte) ([]byte, error) {
	model, ans, u, e := Decode(raw)
	if e != nil {
		return nil, e
	}
	if model == "" {
		model = requestedModel
	}
	return JevResponse(p, model, ans, u)
}

// Result is one HTTP attempt against the Decisions API.
type Result struct {
	Status     int
	Raw        []byte // provider body, unmodified
	Normalized []byte // Jev-shaped body; nil unless Status is 2xx and parsing succeeded
	ParseError string
	Seconds    float64 // HTTP round trip only; translation time is excluded
	RetryAfter int
	WireBody   []byte
}

// Client sends Jev bodies to the Decisions API. It holds no retry policy:
// each experiment keeps its own receipts, retries and budget accounting.
type Client struct {
	HTTP     *http.Client
	Token    string
	Model    string
	Endpoint string
}

// Do translates and sends one Jev body.
func (c Client) Do(ctx context.Context, jevBody []byte) (Result, error) {
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	url := c.Endpoint
	if url == "" {
		url = Endpoint
	}
	wire, plan, e := Translate(jevBody, model)
	if e != nil {
		return Result{}, e
	}
	r := Result{WireBody: wire}
	req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(wire))
	if e != nil {
		return r, e
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 120 * time.Second}
	}
	t0 := time.Now()
	resp, e := h.Do(req)
	if e != nil {
		r.Seconds = time.Since(t0).Seconds()
		return r, e
	}
	r.Status = resp.StatusCode
	r.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), time.Now())
	r.Raw, e = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	r.Seconds = time.Since(t0).Seconds()
	if e != nil {
		return r, e
	}
	if r.Status >= 200 && r.Status < 300 {
		if n, e := Normalize(plan, model, r.Raw); e != nil {
			r.ParseError = e.Error()
		} else {
			r.Normalized = n
		}
	}
	return r, nil
}

// TokenEnv lists the accepted credential names, in priority order.
var TokenEnv = []string{"OPENAI_DECISIONS_KEY", "OPENAI_API_KEY", "OPENAI_DEV_KEY"}

// Token reads the OpenAI credential from the environment only (never from
// ~/.secrets). The default endpoint, the exe.dev integration, needs none; use
// TokenFor to ask for a credential only when the endpoint needs one.
func Token() (string, error) {
	for _, k := range TokenEnv {
		if t := os.Getenv(k); t != "" {
			return t, nil
		}
	}
	return "", fmt.Errorf("missing credential (%s in the environment; only the direct endpoint %s or a non-integration URL needs it, the default %s does not)", strings.Join(TokenEnv, ", "), DirectEndpoint, Endpoint)
}

// retryAfter parses Retry-After as delay-seconds or an HTTP-date.
func retryAfter(v string, now time.Time) int {
	if n, e := strconv.Atoi(strings.TrimSpace(v)); e == nil && n >= 0 {
		return n
	}
	if t, e := http.ParseTime(v); e == nil && t.After(now) {
		return int(t.Sub(now).Seconds() + 0.999)
	}
	return 0
}
