package subq

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Decisions limits. SubQ documents the choice and score limits and a 13 MiB
// request-body limit. The live endpoint additionally rejected 17+ questions
// with HTTP 422 on 2026-10-13; enforce that observed limit before sending.
const (
	MaxQuestions     = 16 // observed server validation; absent from public docs
	MaxChoiceOptions = 255
	MinScoreLevels   = 2
	MaxScoreLevels   = 10
	MaxStateBytes    = 13 << 20
	MaxBodyBytes     = 13 << 20
)

// LimitError is a body that exceeds a documented System One limit. Such a
// body is recorded as a service limit: it is not sent and not split.
type LimitError struct {
	Limit  string // questions, choice_options, score_levels, state_bytes, body_bytes
	Max    int
	Got    int
	Detail string
}

func (e *LimitError) Error() string { return "service limit " + e.Limit + ": " + e.Detail }

// IsServiceLimit reports whether err is (or wraps) a *LimitError.
func IsServiceLimit(err error) bool {
	var l *LimitError
	return errors.As(err, &l)
}

// LimitOf returns the *LimitError in err, or nil.
func LimitOf(err error) *LimitError {
	var l *LimitError
	if errors.As(err, &l) {
		return l
	}
	return nil
}

// PrepareVersion names the body-preparation rules. Receipts record it, and a
// run directory refuses receipts made under a different version.
//
//	v1: keep state and questions bytes and member order; set model (first if
//	    absent); drop every other top-level member (recorded as an edit).
const PrepareVersion = "v1"

// Digest is the lowercase hex SHA-256 of b.
func Digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type member struct {
	key string
	val json.RawMessage
}

// objectMembers decodes a JSON object into its members in document order,
// keeping each value's bytes verbatim.
func objectMembers(b []byte) ([]member, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	if t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var out []member
	seen := map[string]bool{}
	for d.More() {
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		k, _ := t.(string)
		var raw json.RawMessage
		if e := d.Decode(&raw); e != nil {
			return nil, e
		}
		if seen[k] {
			return nil, fmt.Errorf("duplicate key %q", k)
		}
		seen[k] = true
		out = append(out, member{k, raw})
	}
	if _, e := d.Token(); e != nil {
		return nil, e
	}
	if d.More() {
		return nil, errors.New("trailing data after object")
	}
	return out, nil
}

// Edit is one recorded change Prepare made to a frozen body.
type Edit struct {
	Op    string `json:"op"`    // set_model, insert_model, drop_field
	Field string `json:"field"` // top-level member name
	From  string `json:"from,omitempty"`
}

// keptTop are the top-level members SubQ documents.
var keptTop = map[string]bool{"model": true, "state": true, "questions": true}

// Prepare turns a frozen Jev body into the body sent to SubQ. See
// PrepareEdits.
func (c Config) Prepare(frozen []byte) ([]byte, error) {
	b, _, e := c.PrepareEdits(frozen)
	return b, e
}

// PrepareEdits keeps the state and questions members in their original order
// with their original bytes (question order is kept even though the response
// is a map), sets "model" to the configured name (inserted first when
// absent), and drops every other top-level member: the API documents only
// model, state and questions, rejects reasoning_effort other than "none" and
// images are not used by this suite, and other unsupported fields are dropped. No frozen body
// has such a member; when one does, the drop is returned as an Edit. It fails
// when no model is configured or the body would be rejected by the server's
// documented validation (see Validate).
func (c Config) PrepareEdits(frozen []byte) ([]byte, []Edit, error) {
	c = c.Resolve()
	if e := c.RequireModel(); e != nil {
		return nil, nil, e
	}
	if e := Validate(frozen); e != nil {
		return nil, nil, e
	}
	ms, _ := objectMembers(frozen)
	model, _ := json.Marshal(c.Model)
	var edits []Edit
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	put := func(k string, v []byte) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(v)
	}
	hasModel := false
	for _, m := range ms {
		if m.key == "model" {
			hasModel = true
		}
	}
	if !hasModel {
		put("model", model)
		edits = append(edits, Edit{Op: "insert_model", Field: "model"})
	}
	for _, m := range ms {
		switch {
		case m.key == "model":
			put("model", model)
			var old string
			json.Unmarshal(m.val, &old)
			edits = append(edits, Edit{Op: "set_model", Field: "model", From: old})
		case keptTop[m.key]:
			put(m.key, m.val)
		default:
			edits = append(edits, Edit{Op: "drop_field", Field: m.key})
		}
	}
	buf.WriteByte('}')
	if buf.Len() > MaxBodyBytes {
		return nil, nil, &LimitError{Limit: "body_bytes", Max: MaxBodyBytes, Got: buf.Len(), Detail: fmt.Sprintf("prepared body is %d bytes, over the %d-byte limit", buf.Len(), MaxBodyBytes)}
	}
	return buf.Bytes(), edits, nil
}

// Question is a parsed question of a Jev body, in document order.
type Question struct {
	Key     string
	Type    string   // noul, choice or score
	Options []string // choice: criteria keys in order; score: "0".."n-1"; noul: none
	Extra   []string // question fields other than type, instructions, criteria
}

// Questions parses a body's questions in document order and applies the
// server's documented and observed validation. Top-level members other than
// model, state and questions are not errors here (Prepare drops them).
func Questions(body []byte) ([]Question, error) {
	ms, e := objectMembers(body)
	if e != nil {
		return nil, fmt.Errorf("request body: %w", e)
	}
	var qs json.RawMessage
	hasState := false
	for _, m := range ms {
		switch m.key {
		case "state":
			hasState = true
			if len(m.val) > MaxStateBytes {
				return nil, &LimitError{Limit: "state_bytes", Max: MaxStateBytes, Got: len(m.val), Detail: fmt.Sprintf("state is %d bytes, over the %d-byte limit", len(m.val), MaxStateBytes)}
			}
		case "questions":
			qs = m.val
		case "model":
			var s string
			if json.Unmarshal(m.val, &s) != nil {
				return nil, errors.New("model must be a string")
			}
		}
	}
	if !hasState {
		return nil, errors.New("state is required")
	}
	if qs == nil {
		return nil, errors.New("questions is required")
	}
	qm, e := objectMembers(qs)
	if e != nil || len(qm) == 0 {
		return nil, errors.New("questions must be a non-empty object")
	}
	if len(qm) > MaxQuestions {
		return nil, &LimitError{Limit: "questions", Max: MaxQuestions, Got: len(qm), Detail: fmt.Sprintf("questions per request: at most %d, got %d", MaxQuestions, len(qm))}
	}
	var out []Question
	for _, m := range qm {
		q, e := parseQuestion(m.key, m.val)
		if e != nil {
			return nil, e
		}
		out = append(out, q)
	}
	return out, nil
}

func parseQuestion(key string, raw json.RawMessage) (Question, error) {
	fs, e := objectMembers(raw)
	if e != nil {
		return Question{}, fmt.Errorf("question %q must be an object", key)
	}
	q := Question{Key: key}
	var criteria json.RawMessage
	for _, f := range fs {
		switch f.key {
		case "type":
			json.Unmarshal(f.val, &q.Type)
		case "instructions":
		case "criteria":
			if string(f.val) != "null" {
				criteria = f.val
			}
		default:
			q.Extra = append(q.Extra, f.key)
		}
	}
	switch q.Type {
	case "noul":
		if criteria != nil {
			if _, e := objectMembers(criteria); e != nil {
				return q, fmt.Errorf("question %q: noul criteria must be an object", key)
			}
		}
	case "choice":
		cm, e := objectMembers(criteria)
		if criteria == nil || e != nil || len(cm) < 1 {
			return q, fmt.Errorf("question %q: choice criteria must be a non-empty map of option to description", key)
		}
		if len(cm) > MaxChoiceOptions {
			return q, &LimitError{Limit: "choice_options", Max: MaxChoiceOptions, Got: len(cm), Detail: fmt.Sprintf("question %q: options per question: at most %d, got %d", key, MaxChoiceOptions, len(cm))}
		}
		for _, c := range cm {
			q.Options = append(q.Options, c.key)
		}
	case "score":
		var list []json.RawMessage
		if criteria == nil || json.Unmarshal(criteria, &list) != nil {
			return q, fmt.Errorf("question %q: score criteria must be an ordered list of levels", key)
		}
		if len(list) < MinScoreLevels || len(list) > MaxScoreLevels {
			return q, &LimitError{Limit: "score_levels", Max: MaxScoreLevels, Got: len(list),
				Detail: fmt.Sprintf("question %q: score requires %d to %d levels, got %d", key, MinScoreLevels, MaxScoreLevels, len(list))}
		}
		for i := range list {
			q.Options = append(q.Options, strconv.Itoa(i))
		}
	default:
		return q, fmt.Errorf("question %q: unknown question type %q (SubQ accepts noul, choice, score)", key, q.Type)
	}
	return q, nil
}

// Validate applies the server's request validation offline, so incompatible
// frozen bodies are found before any request is sent. ValidateLive checks a
// sample against the server itself without inference.
func Validate(body []byte) error {
	_, e := Questions(body)
	return e
}

// Counts returns the question count and state bytes of a body (0, 0 if it
// does not parse).
func Counts(body []byte) (questions, stateBytes int) {
	ms, e := objectMembers(body)
	if e != nil {
		return 0, 0
	}
	for _, m := range ms {
		switch m.key {
		case "state":
			stateBytes = len(m.val)
		case "questions":
			if qm, e := objectMembers(m.val); e == nil {
				questions = len(qm)
			}
		}
	}
	return
}

// Shape is a coarse signature of a body for sampling distinct shapes:
// question types in order with option counts, plus non-standard fields.
func Shape(body []byte) string {
	qs, e := Questions(body)
	if e != nil {
		return "invalid: " + e.Error()
	}
	ms, _ := objectMembers(body)
	s := ""
	for _, m := range ms {
		if !keptTop[m.key] {
			s += "+" + m.key
		}
		if m.key == "state" && len(m.val) > 0 {
			if m.val[0] == '{' {
				s += "[state:object]"
			} else if m.val[0] == '"' {
				s += "[state:string]"
			} else {
				s += "[state:other]"
			}
		}
	}
	// Collapse repeated identical question signatures so shapes stay few.
	counts := map[string]int{}
	var order []string
	for _, q := range qs {
		sig := q.Type
		if len(q.Options) > 0 {
			sig += strconv.Itoa(len(q.Options))
		}
		if len(q.Extra) > 0 {
			x := append([]string{}, q.Extra...)
			sort.Strings(x)
			sig += fmt.Sprint(x)
		}
		if counts[sig] == 0 {
			order = append(order, sig)
		}
		counts[sig]++
	}
	for _, k := range order {
		s += fmt.Sprintf(" %s×%d", k, counts[k])
	}
	return s
}

// Usage is the usage object of a System One response.
type Usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// Response is the parsed Jev-shaped System One response.
type Response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   *Usage                     `json:"usage"`
}

// ParseResponse checks a 2xx body against the request's questions: every
// question answered with its own type, noul in [0,1], choice among the
// criteria with per-option probabilities and a confidence, score within range
// with per-level probabilities, and usage present.
func ParseResponse(frozen, raw []byte) (Response, error) {
	var r Response
	if e := json.Unmarshal(raw, &r); e != nil {
		return r, fmt.Errorf("response: %w", e)
	}
	if r.Usage == nil {
		return r, errors.New("response has no usage")
	}
	qs, e := Questions(frozen)
	if e != nil {
		return r, e
	}
	if len(r.Answers) != len(qs) {
		return r, fmt.Errorf("response has %d answers for %d questions", len(r.Answers), len(qs))
	}
	for _, q := range qs {
		a, ok := r.Answers[q.Key]
		if !ok {
			return r, fmt.Errorf("no answer for %q", q.Key)
		}
		if e := checkAnswer(q, a); e != nil {
			return r, fmt.Errorf("answer %q: %w", q.Key, e)
		}
	}
	return r, nil
}

func checkAnswer(q Question, raw json.RawMessage) error {
	var a struct {
		Type          string             `json:"type"`
		Noul          *float64           `json:"noul"`
		Choice        *string            `json:"choice"`
		Score         *float64           `json:"score"`
		Confidence    *float64           `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return e
	}
	if a.Type != q.Type {
		return fmt.Errorf("type %q, want %q", a.Type, q.Type)
	}
	unit := func(v float64) bool { return !math.IsNaN(v) && v >= 0 && v <= 1 }
	switch q.Type {
	case "noul":
		if a.Noul == nil || !unit(*a.Noul) {
			return errors.New("noul missing or outside [0,1]")
		}
		return nil
	case "choice":
		if a.Choice == nil || !contains(q.Options, *a.Choice) {
			return errors.New("choice missing or not a criterion")
		}
	case "score":
		if a.Score == nil || math.IsNaN(*a.Score) || *a.Score < 0 || *a.Score > float64(len(q.Options)-1) {
			return errors.New("score missing or out of range")
		}
	}
	// confidence is documented for choice and score but nothing downstream
	// needs it; it is checked when present and its absence is tolerated.
	if a.Confidence != nil && !unit(*a.Confidence) {
		return errors.New("confidence outside [0,1]")
	}
	if len(a.Probabilities) != len(q.Options) {
		return fmt.Errorf("%d probabilities for %d options", len(a.Probabilities), len(q.Options))
	}
	sum := 0.0
	for _, k := range q.Options {
		p, ok := a.Probabilities[k]
		if !ok || !unit(p) {
			return fmt.Errorf("probability for %q missing or outside [0,1]", k)
		}
		sum += p
	}
	// Rounding is undocumented; allow 0.01 plus 0.005 per option of drift.
	if math.Abs(sum-1) > 0.01+0.005*float64(len(q.Options)) {
		return fmt.Errorf("probabilities sum to %.3f", sum)
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
