package decisions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const jevBody = `{
  "model": "jev-1.13.0",
  "questions": {
    "z_useful": {"type": "noul", "instructions": "Useful?"},
    "a_role": {"type": "choice", "instructions": "Role?", "criteria": {"direct": "Direct.", "irrelevant": "None."}},
    "m_rate": {"type": "score", "instructions": "Rate?", "criteria": ["low", "mid", "high"]}
  },
  "state": {
    "query": "q",
    "passages": {"p001": "text"}
  }
}
`

func TestParsePlanKeepsOrderAndState(t *testing.T) {
	p, e := ParsePlan([]byte(jevBody))
	if e != nil {
		t.Fatal(e)
	}
	if got := strings.Join(p.Keys(), ","); got != "z_useful,a_role,m_rate" {
		t.Fatalf("order %s", got)
	}
	if p.Questions[1].Options[0].Value != "direct" || p.Questions[1].Options[1].Value != "irrelevant" {
		t.Fatalf("criteria order %+v", p.Questions[1].Options)
	}
	if !strings.HasPrefix(p.Context, "{\n    \"query\"") {
		t.Fatalf("state bytes not preserved: %q", p.Context)
	}
	s, _ := ParsePlan([]byte(`{"state":"plain text","questions":{"x":{"type":"noul","instructions":"?"}}}`))
	if s.Context != "plain text" {
		t.Fatalf("string state %q", s.Context)
	}
}

func TestRejectsUnknownType(t *testing.T) {
	if _, e := ParsePlan([]byte(`{"state":"s","questions":{"x":{"type":"string"}}}`)); e == nil {
		t.Fatal("expected error")
	}
}

func TestClassify(t *testing.T) {
	for want, c := range map[string]struct {
		s int
		m string
	}{
		AccessNotEnabled: {403, "Decision API is not enabled for this user."},
		AccessOpen:       {400, "Missing required parameter: 'questions'."},
		AccessMissing:    {404, "Invalid URL"},
		AccessAuthError:  {401, "Incorrect API key"},
	} {
		if g := Classify(c.s, c.m); g != want {
			t.Fatalf("%v -> %s, want %s", c, g, want)
		}
	}
}

func TestMatchesModel(t *testing.T) {
	if !MatchesModel("openai-decisions/gpt-6-luna", "gpt-6-luna") || !MatchesModel("openai-decisions/gpt-6-luna-2026-09-29", "gpt-6-luna") {
		t.Fatal("should match")
	}
	if MatchesModel("openai-decisions/gpt-6-luna2", "gpt-6-luna") || MatchesModel("jev-1.13.0", "gpt-6-luna") {
		t.Fatal("should not match")
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 29, 20, 0, 0, 0, time.UTC)
	if retryAfter("7", now) != 7 || retryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now) != 3 || retryAfter("junk", now) != 0 {
		t.Fatal("retryAfter")
	}
}

// liveShape is the documented response shape (guide, 2026-10-06), with the
// usage object as returned by the live endpoint.
const liveShape = `{"model":"gpt-6-luna","answers":[
 {"type":"predicate","name":"z_useful","probability":0.2},
 {"type":"choice","name":"a_role","choice":"direct","probabilities":[{"value":"direct","probability":0.9},{"value":"irrelevant","probability":0.1}],"confidence":0.85},
 {"type":"score","name":"m_rate","score":1.1,"probabilities":[{"value":0,"label":"low","probability":0.1},{"value":1,"label":"mid","probability":0.7},{"value":2,"label":"high","probability":0.2}],"confidence":0.55}],
 "usage":{"input_tokens":149,"input_tokens_details":{"cached_tokens":0},"output_tokens":0,"total_tokens":149}}`

func TestEncodeDocumentedRequest(t *testing.T) {
	w, _, e := Translate([]byte(jevBody), DefaultModel)
	if e != nil {
		t.Fatal(e)
	}
	var r struct {
		Model     string
		Input     string
		Questions []struct {
			Type, Name, Instructions string
			Choices                  []struct{ Value, Description string }
			Levels                   []struct{ Label string }
		}
	}
	if e := json.Unmarshal(w, &r); e != nil {
		t.Fatal(e)
	}
	q := r.Questions
	if r.Model != "gpt-6-luna" || !strings.Contains(r.Input, `"query"`) || len(q) != 3 {
		t.Fatalf("request %s", w)
	}
	if q[0].Type != "predicate" || q[0].Name != "z_useful" || q[1].Type != "choice" || q[1].Choices[0].Description != "Direct." || q[2].Type != "score" || q[2].Levels[2].Label != "high" {
		t.Fatalf("questions %s", w)
	}
	if strings.Contains(string(w), "criteria") {
		t.Fatalf("criteria is not a Decisions field: %s", w)
	}
}

func TestNormalizeJevShapes(t *testing.T) {
	p, _ := ParsePlan([]byte(jevBody))
	b, e := Normalize(p, DefaultModel, []byte(liveShape))
	if e != nil {
		t.Fatal(e)
	}
	var r struct {
		Model   string
		Answers map[string]map[string]any
		Usage   map[string]int
	}
	json.Unmarshal(b, &r)
	if r.Model != "openai-decisions/gpt-6-luna" || r.Usage["input_tokens"] != 149 {
		t.Fatalf("%s", b)
	}
	if r.Answers["z_useful"]["noul"].(float64) != 0.2 {
		t.Fatalf("noul %v", r.Answers["z_useful"])
	}
	c := r.Answers["a_role"]
	if c["choice"] != "direct" || c["probabilities"].(map[string]any)["irrelevant"].(float64) != 0.1 || c["confidence"].(float64) != 0.85 {
		t.Fatalf("choice %v", c)
	}
	s := r.Answers["m_rate"]
	if s["score"].(float64) != 1.1 || s["probabilities"].(map[string]any)["1"].(float64) != 0.7 || s["legend"].(map[string]any)["2"] != "high" {
		t.Fatalf("score %v", s)
	}
}

func TestRefusalIsKeptVisible(t *testing.T) {
	p, _ := ParsePlan([]byte(jevBody))
	raw := strings.Replace(liveShape, `{"type":"predicate","name":"z_useful","probability":0.2}`, `{"type":"refusal","name":"z_useful"}`, 1)
	b, e := Normalize(p, DefaultModel, []byte(raw))
	if e != nil || !strings.Contains(string(b), `"z_useful":{"type":"refusal"}`) {
		t.Fatalf("%v %s", e, b)
	}
}

func TestNormalizeRejectsBadAnswers(t *testing.T) {
	p, _ := ParsePlan([]byte(jevBody))
	for name, bad := range map[string][2]string{
		"off-option choice":   {`"choice":"direct"`, `"choice":"maybe"`},
		"probability above 1": {`"probability":0.2}`, `"probability":1.2}`},
		"score out of range":  {`"score":1.1`, `"score":2.5`},
		"wrong answer type":   {`{"type":"predicate","name":"z_useful"`, `{"type":"choice","name":"z_useful"`},
		"duplicate answer":    {`{"type":"choice","name":"a_role"`, `{"type":"predicate","name":"z_useful","probability":0.1},{"type":"choice","name":"a_role"`},
		"missing answer":      {`{"type":"predicate","name":"z_useful","probability":0.2},`, ``},
	} {
		if _, e := Normalize(p, "m", []byte(strings.Replace(liveShape, bad[0], bad[1], 1))); e == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestMissingUsageStaysMissing(t *testing.T) {
	p, _ := ParsePlan([]byte(jevBody))
	raw := liveShape[:strings.Index(liveShape, `,
 "usage"`)] + "}"
	b, e := Normalize(p, "m", []byte(raw))
	if e != nil || strings.Contains(string(b), "usage") {
		t.Fatalf("%v %s", e, b)
	}
}

func TestNoulCriteriaGoIntoPredicateInstructions(t *testing.T) {
	p, e := ParsePlan([]byte(`{"state":"s","questions":{"x":{"type":"noul","instructions":"Q?","criteria":{"true":"T.","false":"F."}}}}`))
	if e != nil {
		t.Fatal(e)
	}
	if got := PredicateInstructions(p.Questions[0]); got != "Q?\n\nCount as true: T.\n\nCount as false: F." {
		t.Fatalf("%q", got)
	}
	if _, e := ParsePlan([]byte(`{"state":"s","questions":{"x":{"type":"noul","criteria":{"maybe":"?"}}}}`)); e == nil {
		t.Fatal("bad criterion accepted")
	}
}

func TestClientDo(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing auth")
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write([]byte(liveShape))
	}))
	defer srv.Close()
	r, e := Client{Token: "tok", Endpoint: srv.URL}.Do(context.Background(), []byte(jevBody))
	if e != nil || r.Status != 200 || r.ParseError != "" {
		t.Fatal(e, r.Status, r.ParseError)
	}
	if got.Model != DefaultModel || len(got.Questions) != 3 || got.Questions[0].Name != "z_useful" {
		t.Fatalf("wire %+v", got)
	}
	if !strings.Contains(string(r.Normalized), `"noul":0.2`) {
		t.Fatalf("normalized %s", r.Normalized)
	}
}
