// Package decisions lets the Jev experiments send their frozen Jev
// requests to the OpenAI Decisions API and read the answers back in Jev's
// response shape, so every downstream parser, replay and report stays as-is.
//
// The translation is split in two layers:
//
//   - adapter.go (this file) is provider-neutral. It parses a Jev body into
//     a Plan (context text plus ordered questions with their allowed answers)
//     and turns provider answers back into Jev-shaped answers.
//   - wire.go holds the OpenAI request/response encoding. It is the only file
//     that depends on the Decisions API's wire format.
package decisions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Provider names used in configs, flags and receipts.
const (
	ProviderJev       = "jev"
	ProviderDecisions = "openai-decisions"
)

// Yes and No are the two answers a noul question is translated into.
const (
	Yes = "yes"
	No  = "no"
)

// Option is one allowed answer of a question.
type Option struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// Question is one Jev question in provider-neutral form.
type Question struct {
	Key          string   `json:"key"`
	JevType      string   `json:"jev_type"` // "noul", "choice" or "score"
	Instructions string   `json:"instructions"`
	Options      []Option `json:"options"`
}

// Plan is a parsed Jev request.
type Plan struct {
	JevModel  string     `json:"jev_model"`
	Context   string     `json:"context"`
	Questions []Question `json:"questions"`
}

// Answer is one provider answer before conversion back to Jev's shape.
type Answer struct {
	Key         string
	Type        string             // provider answer type: predicate, choice, score or refusal
	Value       string             // choice: the selected option
	Probability *float64           // predicate: probability the condition is true
	Score       *float64           // score: probability-weighted level index
	Confidence  *float64           // choice/score: reported confidence
	Scores      map[string]float64 // choice: option -> probability; score: level index -> probability
	LevelLabels []string           // score: labels as echoed, in index order
}

// Refusal is the provider answer type for a question the model declined.
const Refusal = "refusal"

// Usage is billable token usage reported by the provider.
type Usage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	Reported     bool `json:"-"` // false when the provider sent no usage object
}

// ParsePlan reads a Jev request body. Question order follows the body so
// that ordered-fan-out studies keep their order on the wire.
func ParsePlan(body []byte) (Plan, error) {
	var raw struct {
		Model     string          `json:"model"`
		State     json.RawMessage `json:"state"`
		Questions json.RawMessage `json:"questions"`
	}
	if e := json.Unmarshal(body, &raw); e != nil {
		return Plan{}, fmt.Errorf("jev body: %w", e)
	}
	if len(raw.State) == 0 {
		return Plan{}, fmt.Errorf("jev body has no state")
	}
	ctx, e := contextText(raw.State)
	if e != nil {
		return Plan{}, e
	}
	keys, e := orderedKeys(raw.Questions)
	if e != nil {
		return Plan{}, fmt.Errorf("jev questions: %w", e)
	}
	var qs map[string]struct {
		Type         string            `json:"type"`
		Instructions string            `json:"instructions"`
		RawCriteria  json.RawMessage   `json:"criteria"`
		Criteria     map[string]string `json:"-"`
	}
	if e := json.Unmarshal(raw.Questions, &qs); e != nil {
		return Plan{}, fmt.Errorf("jev questions: %w", e)
	}
	p := Plan{JevModel: raw.Model, Context: ctx}
	for _, k := range keys {
		q := qs[k]
		var levels []string
		if q.Type == "score" {
			// Jev score criteria are an ordered list of level labels.
			if e := json.Unmarshal(q.RawCriteria, &levels); e != nil {
				return Plan{}, fmt.Errorf("score question %q: criteria must be a list of levels: %w", k, e)
			}
		} else if len(q.RawCriteria) > 0 && string(q.RawCriteria) != "null" {
			if e := json.Unmarshal(q.RawCriteria, &q.Criteria); e != nil {
				return Plan{}, fmt.Errorf("question %q criteria: %w", k, e)
			}
		}
		out := Question{Key: k, JevType: q.Type, Instructions: q.Instructions}
		switch q.Type {
		case "noul":
			// Optional true/false criteria become the yes/no descriptions.
			for c := range q.Criteria {
				if c != "true" && c != "false" {
					return Plan{}, fmt.Errorf("noul question %q has unsupported criterion %q", k, c)
				}
			}
			out.Options = []Option{{Value: Yes, Description: q.Criteria["true"]}, {Value: No, Description: q.Criteria["false"]}}
		case "choice":
			if len(q.Criteria) < 2 {
				return Plan{}, fmt.Errorf("choice question %q needs at least two criteria", k)
			}
			// Criteria order is taken from the body, not from Go's sorted map.
			ck, e := criteriaOrder(raw.Questions, k)
			if e != nil {
				return Plan{}, e
			}
			for _, c := range ck {
				out.Options = append(out.Options, Option{Value: c, Description: q.Criteria[c]})
			}
		case "score":
			if len(levels) < 2 {
				return Plan{}, fmt.Errorf("score question %q needs at least two levels", k)
			}
			for _, l := range levels {
				out.Options = append(out.Options, Option{Value: l})
			}
		default:
			return Plan{}, fmt.Errorf("question %q has unsupported type %q", k, q.Type)
		}
		p.Questions = append(p.Questions, out)
	}
	if len(p.Questions) == 0 {
		return Plan{}, fmt.Errorf("jev body has no questions")
	}
	return p, nil
}

// contextText passes string states through verbatim and keeps structured
// states as the exact JSON bytes the Jev study froze (indentation included).
func contextText(state json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(state, &s) == nil {
		return s, nil
	}
	if !json.Valid(state) {
		return "", fmt.Errorf("jev state is not valid JSON")
	}
	return string(bytes.TrimSpace(state)), nil
}

func orderedKeys(obj json.RawMessage) ([]string, error) {
	d := json.NewDecoder(bytes.NewReader(obj))
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	if t != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	var keys []string
	for d.More() {
		t, e := d.Token()
		if e != nil {
			return nil, e
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if e := d.Decode(&skip); e != nil {
			return nil, e
		}
	}
	return keys, nil
}

func criteriaOrder(questions json.RawMessage, key string) ([]string, error) {
	var qs map[string]struct {
		Criteria json.RawMessage `json:"criteria"`
	}
	if e := json.Unmarshal(questions, &qs); e != nil {
		return nil, e
	}
	return orderedKeys(qs[key].Criteria)
}

// JevResponse builds a Jev-shaped response from provider answers, in
// the shapes Jev returns: noul {noul}; choice {choice, probabilities by
// option, confidence}; score {score, probabilities and legend by level
// index, confidence}. Decisions predicates return a probability directly, so
// noul needs no approximation. A refusal is kept as {"type": "refusal"} so
// callers see and count it instead of a fabricated value.
func JevResponse(p Plan, model string, answers []Answer, u Usage) ([]byte, error) {
	by := map[string]Answer{}
	for _, a := range answers {
		if _, dup := by[a.Key]; dup {
			return nil, fmt.Errorf("provider answered %q more than once", a.Key)
		}
		by[a.Key] = a
	}
	if len(by) != len(p.Questions) {
		return nil, fmt.Errorf("provider returned %d answers for %d questions", len(by), len(p.Questions))
	}
	want := map[string]string{"noul": "predicate", "choice": "choice", "score": "score"}
	out := map[string]any{}
	for _, q := range p.Questions {
		a, ok := by[q.Key]
		if !ok {
			return nil, fmt.Errorf("provider returned no answer for %q", q.Key)
		}
		if a.Type == Refusal {
			out[q.Key] = map[string]any{"type": Refusal}
			continue
		}
		if a.Type != want[q.JevType] {
			return nil, fmt.Errorf("answer for %q has type %q, want %q", q.Key, a.Type, want[q.JevType])
		}
		if a.Confidence != nil && !unit(*a.Confidence) {
			return nil, fmt.Errorf("invalid confidence %v for %q", *a.Confidence, q.Key)
		}
		for k, v := range a.Scores {
			if !unit(v) {
				return nil, fmt.Errorf("invalid probability %q=%v for %q", k, v, q.Key)
			}
		}
		switch q.JevType {
		case "noul":
			if a.Probability == nil || !unit(*a.Probability) {
				return nil, fmt.Errorf("predicate %q has no valid probability", q.Key)
			}
			out[q.Key] = map[string]any{"type": "noul", "noul": *a.Probability}
		case "choice":
			if !allowed(q, a.Value) {
				return nil, fmt.Errorf("provider answer %q for %q is not an allowed option", a.Value, q.Key)
			}
			for k := range a.Scores {
				if !allowed(q, k) {
					return nil, fmt.Errorf("probability for unknown option %q in %q", k, q.Key)
				}
			}
			m := map[string]any{"type": "choice", "choice": a.Value}
			if a.Confidence != nil {
				m["confidence"] = *a.Confidence
			}
			if len(a.Scores) > 0 {
				m["probabilities"], m["scores"] = a.Scores, a.Scores
			}
			out[q.Key] = m
		case "score":
			n := len(q.Options)
			if a.Score == nil || math.IsNaN(*a.Score) || *a.Score < 0 || *a.Score > float64(n-1) {
				return nil, fmt.Errorf("score %q has no valid score", q.Key)
			}
			legend := map[string]string{}
			for i, o := range q.Options {
				legend[strconv.Itoa(i)] = o.Value
			}
			for k := range a.Scores {
				if _, ok := legend[k]; !ok {
					return nil, fmt.Errorf("probability for unknown level %q in %q", k, q.Key)
				}
			}
			m := map[string]any{"type": "score", "score": *a.Score, "legend": legend}
			if a.Confidence != nil {
				m["confidence"] = *a.Confidence
			}
			if len(a.Scores) > 0 {
				m["probabilities"] = a.Scores
			}
			out[q.Key] = m
		}
	}
	resp := map[string]any{
		"model":    ModelLabel(model),
		"provider": ProviderDecisions,
		"answers":  out,
	}
	// Absent usage stays absent so callers never price a request as free.
	if u.Reported {
		resp["usage"] = u
	}
	return json.Marshal(resp)
}

// ModelLabel is the model string placed in normalized responses and
// manifests, so replays can tell providers apart.
func ModelLabel(model string) string { return ProviderDecisions + "/" + model }

// MatchesModel reports whether a normalized response's model label belongs to
// the requested model, allowing a dated snapshot suffix
// ("openai-decisions/gpt-6-luna-2026-09-29" matches "gpt-6-luna").
func MatchesModel(label, requested string) bool {
	want := ModelLabel(requested)
	return label == want || strings.HasPrefix(label, want+"-")
}

func allowed(q Question, v string) bool {
	for _, o := range q.Options {
		if o.Value == v {
			return true
		}
	}
	return false
}

func unit(v float64) bool { return !math.IsNaN(v) && v >= 0 && v <= 1 }

// Keys returns the question keys in request order.
func (p Plan) Keys() []string {
	k := make([]string, len(p.Questions))
	for i, q := range p.Questions {
		k[i] = q.Key
	}
	return k
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
