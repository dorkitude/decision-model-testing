package fwsystemone_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/fireworks-systemone-adapter/fwsystemone"
	"github.com/dorkitude/decision-model-testing/experiments/fireworks-systemone-adapter/fwsystemone/fwfake"
)

const frozen = `{"model": "jev-1.13.0", "state": {"passage": "x  y", "question": "q"},
 "questions": {"z_last": {"type": "noul", "instructions": "Z?"}, "a_first": {"type": "choice", "instructions": "A?", "criteria": {"b": "B", "a": null}}, "s": {"type": "score", "instructions": "S?", "criteria": ["lo", "mid", "hi"]}}}`

func cfg(url string) fwsystemone.Config {
	return fwsystemone.Config{URL: url, APIKey: "test-key", Model: fwfake.DefaultModel, Backoff: time.Millisecond}.Resolve()
}

func TestPrepareKeepsBytesAndOrder(t *testing.T) {
	c := cfg("")
	sent, edits, e := c.PrepareEdits([]byte(frozen))
	if e != nil {
		t.Fatal(e)
	}
	want := `{"model":"fake-systemone","state":{"passage": "x  y", "question": "q"},"questions":{"z_last": {"type": "noul", "instructions": "Z?"}, "a_first": {"type": "choice", "instructions": "A?", "criteria": {"b": "B", "a": null}}, "s": {"type": "score", "instructions": "S?", "criteria": ["lo", "mid", "hi"]}}}`
	if string(sent) != want {
		t.Fatalf("sent:\n%s\nwant:\n%s", sent, want)
	}
	if len(edits) != 1 || edits[0].Op != "set_model" || edits[0].From != "jev-1.13.0" {
		t.Fatalf("edits %+v", edits)
	}
	extra := `{"state":"s","questions":{"q":{"type":"noul","instructions":"?"}},"options":{"think":false},"reasoning_effort":"low"}`
	sent, edits, e = c.PrepareEdits([]byte(extra))
	if e != nil {
		t.Fatal(e)
	}
	if string(sent) != `{"model":"fake-systemone","state":"s","questions":{"q":{"type":"noul","instructions":"?"}}}` {
		t.Fatalf("sent %s", sent)
	}
	if len(edits) != 3 || edits[0].Op != "insert_model" || edits[1].Field != "options" || edits[2].Field != "reasoning_effort" {
		t.Fatalf("edits %+v", edits)
	}
	if _, e := (fwsystemone.Config{}).Prepare([]byte(frozen)); e == nil || !strings.Contains(e.Error(), "FIREWORKS_SYSTEMONE_MODEL") {
		t.Fatalf("missing model accepted: %v", e)
	}
}

func TestValidateMatchesServerRules(t *testing.T) {
	many := map[string]any{}
	for i := 0; i < 257; i++ {
		many["q"+string(rune('a'+i%26))+strings.Repeat("x", i/26)] = map[string]any{"type": "noul", "instructions": "?"}
	}
	mb, _ := json.Marshal(map[string]any{"state": "s", "questions": many})
	opts := map[string]string{}
	for i := 0; i < 209; i++ {
		opts["o"+strings.Repeat("x", i)] = "x"
	}
	ob, _ := json.Marshal(map[string]any{"state": "s", "questions": map[string]any{"q": map[string]any{"type": "choice", "criteria": opts}}})
	bad := map[string]string{
		"no state":        `{"questions":{"q":{"type":"noul"}}}`,
		"empty questions": `{"state":"s","questions":{}}`,
		"bool type":       `{"state":"s","questions":{"q":{"type":"bool","instructions":"?"}}}`,
		"choice list":     `{"state":"s","questions":{"q":{"type":"choice","criteria":["a"]}}}`,
		"score 1 level":   `{"state":"s","questions":{"q":{"type":"score","criteria":["a"]}}}`,
		"score 11 levels": `{"state":"s","questions":{"q":{"type":"score","criteria":["1","2","3","4","5","6","7","8","9","10","11"]}}}`,
		"257 questions":   string(mb),
		"209 options":     string(ob),
	}
	for name, b := range bad {
		if fwsystemone.Validate([]byte(b)) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Accepted by the live server's pre-model validation (2026-10-02).
	for _, ok := range []string{
		`{"state":null,"questions":{"q":{"type":"noul"}}}`,
		`{"state":"s","questions":{"q":{"type":"noul","instructions":"?","criteria":{"yes":"y"}}}}`,
		`{"state":"s","questions":{"q":{"type":"noul","instructions":"?","description":"x"}}}`,
		`{"state":"s","questions":{"q":{"type":"choice","criteria":{"a":null,"b":"B"}}}}`,
		frozen,
	} {
		if e := fwsystemone.Validate([]byte(ok)); e != nil {
			t.Errorf("%s: %v", ok, e)
		}
	}
}

func TestClientAgainstFake(t *testing.T) {
	f := &fwfake.Server{}
	ts := httptest.NewServer(f.Handler())
	defer ts.Close()
	c := cfg(ts.URL + "/inference/v1/systemone")
	res, e := fwsystemone.Client{Config: c}.Do(context.Background(), []byte(frozen))
	if e != nil || !res.OK() {
		t.Fatalf("%v %d %s %s", e, res.Status, res.ParseError, res.Raw)
	}
	if res.FrozenSHA != fwsystemone.Digest([]byte(frozen)) || res.SentSHA != fwsystemone.Digest(res.Sent) || res.RequestID == "" || !res.Fake {
		t.Fatalf("result %+v", res)
	}
	rc := c.Receipt(res, 1, nil)
	if rc.HTTPStatus != 200 || rc.Questions != 3 || rc.StateBytes == 0 || rc.HeaderRequestID == "" || rc.HeaderValues["x-request-id"] == "" || len(rc.HeaderNames) == 0 {
		t.Fatalf("telemetry %+v", rc)
	}
	for _, n := range rc.HeaderNames {
		if strings.Contains(n, "auth") || strings.Contains(n, "cookie") {
			t.Fatalf("secret header recorded: %s", n)
		}
	}
	if rc.Provider != "fireworks-systemone" || rc.ProviderModel != fwfake.DefaultModel || rc.InputTokens == 0 || !rc.USDIsPlaceholder || rc.USD <= 0 || rc.PrepareVersion != "v1" || rc.ServerTime == "" {
		t.Fatalf("receipt %+v", rc)
	}
	// Same state again: the fake reports cached tokens, priced at the cached rate.
	res2, _ := fwsystemone.Client{Config: c}.Do(context.Background(), []byte(frozen))
	if res2.Usage().CachedInputTokens == 0 {
		t.Fatal("no cached tokens on repeated state")
	}
	// Unknown model: 404, permanent, classified as not allowlisted.
	c2 := c
	c2.Model = "someone-elses-model"
	res, _ = fwsystemone.Client{Config: c2}.Do(context.Background(), []byte(frozen))
	if res.Status != 404 || !fwsystemone.NotAllowlisted(res.Status, res.Raw) || fwsystemone.Transient(res.Status) {
		t.Fatalf("unknown model: %d %s", res.Status, res.Raw)
	}
	// Transient failures carry Retry-After.
	f2 := &fwfake.Server{FailEvery: 1}
	ts2 := httptest.NewServer(f2.Handler())
	defer ts2.Close()
	res, _ = fwsystemone.Client{Config: cfg(ts2.URL + "/inference/v1/systemone")}.Do(context.Background(), []byte(frozen))
	if res.Status != 503 || !fwsystemone.Transient(res.Status) || res.RetryAfter != "0" || c.Sleep(1, res.RetryAfter) != 0 {
		t.Fatalf("fail-every: %d %q", res.Status, res.RetryAfter)
	}
	rc = c.Receipt(res, 1, nil)
	if rc.ErrorCode != "SERVICE_UNAVAILABLE" || rc.ErrorMessage == "" || rc.BodyRequestID == "" {
		t.Fatalf("error receipt %+v", rc)
	}
}

func TestDoRetryHistory(t *testing.T) {
	f := &fwfake.Server{FailEvery: 2} // every 2nd valid request: 429 with Retry-After: 0
	ts := httptest.NewServer(f.Handler())
	defer ts.Close()
	c := cfg(ts.URL + "/inference/v1/systemone")
	cl := fwsystemone.Client{Config: c}
	cl.Do(context.Background(), []byte(frozen)) // request 1 succeeds
	var seen []int
	res, hist, e := cl.DoRetry(context.Background(), []byte(frozen), func(r fwsystemone.Result, a int, h []fwsystemone.AttemptRecord) {
		if len(h) != a-1 {
			t.Errorf("attempt %d history %d", a, len(h))
		}
		seen = append(seen, r.Status)
	})
	if e != nil || !res.OK() || len(hist) != 2 || hist[0].Status != 429 || hist[0].Code != "TOO_MANY_REQUESTS" || hist[1].Status != 200 || len(seen) != 2 {
		t.Fatalf("%v %+v %v", e, hist, seen)
	}
	// 404 (model not allowlisted) is permanent: one attempt.
	c404 := c
	c404.Model = "not-allowlisted"
	_, hist, _ = fwsystemone.Client{Config: c404}.DoRetry(context.Background(), []byte(frozen), nil)
	if len(hist) != 1 || hist[0].Status != 404 {
		t.Fatalf("history %+v", hist)
	}
	// A body over a documented limit is never sent.
	opts := map[string]string{}
	for i := 0; i < 209; i++ {
		opts["o"+strings.Repeat("x", i)] = "x"
	}
	big, _ := json.Marshal(map[string]any{"state": "s", "questions": map[string]any{"q": map[string]any{"type": "choice", "criteria": opts}}})
	before := f.Posts()
	_, hist, e = cl.DoRetry(context.Background(), big, nil)
	if !fwsystemone.IsServiceLimit(e) || len(hist) != 0 || f.Posts() != before {
		t.Fatalf("limit: %v %d", e, len(hist))
	}
	lr := c.LimitReceipt(big, e)
	if lr.ServiceLimit == nil || lr.ServiceLimit.Limit != "choice_options" || lr.ServiceLimit.Got != 209 || lr.ErrorCode != "SERVICE_LIMIT" {
		t.Fatalf("limit receipt %+v", lr)
	}
}

func TestDocumentedShapeAndMissingOptionalFields(t *testing.T) {
	// The documented example response, verbatim.
	doc := `{"model":"m","answers":{"refund":{"type":"noul","noul":0.98},"tone":{"type":"choice","choice":"angry","probabilities":{"angry":0.91,"neutral":0.08,"positive":0.01},"confidence":0.87},"urgency":{"type":"score","score":1.7,"legend":{"0":"not urgent","1":"somewhat urgent","2":"urgent","3":"critical"},"probabilities":{"0":0.05,"1":0.3,"2":0.55,"3":0.1},"confidence":0.4}},"usage":{"input_tokens":212,"cached_input_tokens":0,"output_tokens":0}}`
	req := `{"state":"x","questions":{"refund":{"type":"noul","instructions":"?","criteria":{"true":"a","false":"b"}},"tone":{"type":"choice","instructions":"?","criteria":{"angry":"","neutral":"","positive":""}},"urgency":{"type":"score","instructions":"?","criteria":["not urgent","somewhat urgent","urgent","critical"]}}}`
	if _, e := fwsystemone.ParseResponse([]byte(req), []byte(doc)); e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer((&fwfake.Server{OmitOptional: true}).Handler())
	defer ts.Close()
	res, e := fwsystemone.Client{Config: cfg(ts.URL + "/inference/v1/systemone")}.Do(context.Background(), []byte(frozen))
	if e != nil || !res.OK() || strings.Contains(string(res.Raw), "confidence") || strings.Contains(string(res.Raw), "cached_input_tokens") {
		t.Fatalf("%v %s %s", e, res.ParseError, res.Raw)
	}
}

func TestProbeClassifies(t *testing.T) {
	ts := httptest.NewServer((&fwfake.Server{}).Handler())
	defer ts.Close()
	url := ts.URL + "/inference/v1/systemone"
	if r := fwsystemone.Probe(context.Background(), nil, fwsystemone.Config{URL: url, APIKey: "k"}, false); r.Status != fwsystemone.StatusConfigError {
		t.Fatalf("no model: %s", r.Status)
	}
	c := cfg(url)
	c.Model = "not-yet-allowlisted"
	if r := fwsystemone.Probe(context.Background(), nil, c, true); r.Status != fwsystemone.StatusNotAllowlisted || r.Open() || r.Smoke != nil {
		t.Fatalf("not allowlisted: %+v", r)
	}
	r := fwsystemone.Probe(context.Background(), nil, cfg(url), true)
	if r.Status != fwsystemone.StatusOK || !r.Fake || !r.Open() {
		t.Fatalf("smoke: %+v", r)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "test-key") {
		t.Fatal("probe output contains the API key")
	}
}

func TestValidateLiveUsesNonexistentModel(t *testing.T) {
	f := &fwfake.Server{}
	ts := httptest.NewServer(f.Handler())
	defer ts.Close()
	dir := t.TempDir()
	write := func(name, b string) { os.WriteFile(filepath.Join(dir, name), []byte(b), 0o644) }
	write("a.json", frozen)
	write("b.json", `{"model":"jev","state":"s","questions":{"q":{"type":"bool","instructions":"?"}}}`)
	write("c.json", `{"model":"jev","state":"s","questions":{"q":{"type":"noul","instructions":"?"}},"options":{"x":1}}`)
	write("d.json", `{"model":"jev","state":"tiny","questions":{"q":{"type":"noul","instructions":"?"}}}`)
	write("notabody.json", `{"x":1}`)
	s, e := fwsystemone.SelectSamples([]string{dir}, 10)
	if e != nil || len(s) != 4 {
		t.Fatalf("%v %d", e, len(s))
	}
	if !strings.Contains(s[0].Shape, "+options") {
		t.Fatalf("non-standard shapes first: %+v", s)
	}
	checks := fwsystemone.ValidateLive(context.Background(), nil, cfg(ts.URL+"/inference/v1/systemone"), s, 0)
	verdict := map[string]string{}
	for _, c := range checks {
		verdict[filepath.Base(c.Path)] = c.Verdict
	}
	if verdict["a.json"] != "valid" || verdict["b.json"] != "invalid" || verdict["c.json"] != "valid" || verdict["d.json"] != "valid" {
		t.Fatalf("verdicts %v", verdict)
	}
	if f.Posts() != 4 {
		t.Fatalf("posts %d", f.Posts())
	}
}

func TestCost(t *testing.T) {
	c := fwsystemone.Config{USDPerMTokInput: 2, USDPerMTokCached: 0.5, USDPerMTokOutput: 4}.Resolve()
	got := c.USDOf(fwsystemone.Usage{InputTokens: 1_000_000, CachedInputTokens: 400_000, OutputTokens: 100_000})
	if math.Abs(got-(0.6*2+0.4*0.5+0.1*4)) > 1e-9 || c.PlaceholderPrice() {
		t.Fatalf("usd %v", got)
	}
	d := fwsystemone.Config{}.Resolve()
	if !d.PlaceholderPrice() || d.USDOf(fwsystemone.Usage{InputTokens: 500_000, OutputTokens: 500_000}) != 1 {
		t.Fatal("placeholder default")
	}
	t0 := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cost := d.CostOf([]fwsystemone.Span{
		{Started: t0, Seconds: 2, Questions: 3, Usage: fwsystemone.Usage{InputTokens: 100}, OK: true},
		{Started: t0.Add(time.Second), Seconds: 2, Questions: 1, Usage: fwsystemone.Usage{InputTokens: 50}, OK: true},
		{Started: t0.Add(10 * time.Second), Seconds: 1, OK: false},
	})
	if cost.FailuresByStatus["0"] != 1 || cost.P99RoundTrip != 2 || cost.PeakPerMinute != 3 || cost.Questions != 4 || cost.BusySeconds != 4 || cost.SpanSeconds != 11 || cost.InputTokens != 150 {
		t.Fatalf("cost %+v", cost)
	}
}

func TestMatchesModelAndSleep(t *testing.T) {
	c := fwsystemone.Config{Model: "accounts/fireworks/models/system-one"}.Resolve()
	if !c.MatchesModel("system-one") || !c.MatchesModel("accounts/fireworks/models/system-one") || c.MatchesModel("other") {
		t.Fatal("MatchesModel")
	}
	if c.Sleep(1, "") != 2*time.Second || c.Sleep(3, "") != 8*time.Second || c.Sleep(1, "7") != 7*time.Second || c.Sleep(1, "9999") != fwsystemone.MaxRetryAfter {
		t.Fatal("Sleep")
	}
}

func TestValidationModelPassesPrepare(t *testing.T) {
	c := fwsystemone.Config{Model: fwsystemone.ValidationModel}.Resolve()
	if _, e := c.Prepare([]byte(frozen)); e != nil {
		t.Fatal(e)
	}
}
