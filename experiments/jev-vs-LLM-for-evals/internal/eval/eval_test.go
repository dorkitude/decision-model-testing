package eval

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func prices() M {
	return M{"models": M{"m": M{"input_per_million": 2.0, "cached_input_per_million": .25, "output_per_million": 6.0}, "jev-latest": M{"input_per_million": .042, "cached_input_per_million": .042, "output_per_million": 0.0}}}
}
func receipt(model string, usage M) M {
	return M{"model": model, "payload": M{}, "transport_ok": true, "latency_s": 1.0, "response": M{"usage": usage}}
}
func TestPriceCacheReasoningAndFreeOutput(t *testing.T) {
	r := PriceReceipt(receipt("m", M{"prompt_tokens": 1000.0, "completion_tokens": 100.0, "prompt_tokens_details": M{"cached_tokens": 400.0}, "completion_tokens_details": M{"reasoning_tokens": 80.0}}), prices())
	if math.Abs(num(r["estimated_total_usd"])-.0019) > 1e-12 {
		t.Fatal(r)
	}
	if num(r["reasoning_tokens"]) != 80 || num(r["output_tokens"]) != 100 {
		t.Fatal(r)
	}
	j := PriceReceipt(receipt("jev-latest", M{"input_tokens": 1000.0, "output_tokens": 38.0}), prices())
	if num(j["estimated_output_usd"]) != 0 || math.Abs(num(j["estimated_total_usd"])-.000042) > 1e-12 {
		t.Fatal(j)
	}
}
func TestMissingUsageAndUnknownModelStayUnknown(t *testing.T) {
	for _, r := range []M{receipt("m", M{}), receipt("unknown", M{"input_tokens": 1.0, "output_tokens": 1.0})} {
		x := PriceReceipt(r, prices())
		if x["estimated_total_usd"] != nil {
			t.Fatal(x)
		}
		a := aggregate([]M{x})
		if a["estimated_total_usd"] != nil || num(a["unpriced_attempts"]) != 1 {
			t.Fatal(a)
		}
	}
}
func TestHelperOwnershipAndRetryCost(t *testing.T) {
	out := t.TempDir()
	WriteJSON(filepath.Join(out, "jobs.json"), []M{{"key": "job", "benchmark": "llmbar", "method": "Metrics", "model": "jev-latest", "row": M{"subset": "Natural", "id": "1"}}})
	Append(filepath.Join(out, "results.jsonl"), M{"key": "job", "valid": true, "published_score": 1.0})
	for i, model := range []string{"m", "m", "jev-latest"} {
		r := receipt(model, M{"input_tokens": 1000.0, "output_tokens": 10.0})
		r["key"] = "job/aux"
		if i == 2 {
			r["key"] = "job/decision"
		}
		r["started_utc"] = "2026-09-17T00:00:00Z"
		r["attempt"] = i + 1
		r["terminal"] = i != 0
		Append(filepath.Join(out, "requests.jsonl"), r)
	}
	r, e := Economics(out, prices())
	if e != nil {
		t.Fatal(e)
	}
	j := r["jobs"].([]M)[0]
	if num(j["api_attempts"]) != 3 || num(obj(j["helpers"])["api_attempts"]) != 2 {
		t.Fatal(j)
	}
	if math.Abs(num(j["estimated_total_usd"])-.004162) > 1e-12 {
		t.Fatal(j)
	}
}
func TestNativeHTTPResumeAndPayloadGuard(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-only")
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("auth")
		}
		json.NewEncoder(w).Encode(M{"choices": []any{M{"finish_reason": "stop", "message": M{"content": "Output (a)"}}}, "usage": M{"prompt_tokens": 10, "completion_tokens": 3}})
	}))
	defer s.Close()
	out := t.TempDir()
	cfg := M{"timeout_s": 2, "max_requests": 1, "max_tokens_floor": 0}
	c := NewClient(out, cfg, false)
	c.FireworksURL = s.URL
	payload := []any{message("user", "test")}
	if !yes(c.Chat("j/1", "m", payload, 5)["valid"]) {
		t.Fatal("invalid")
	}
	raw := string(Read(filepath.Join(out, "requests.jsonl")))
	if strings.Contains(raw, "test-only") || strings.Contains(strings.ToLower(raw), "authorization") {
		t.Fatal("credential leaked into receipt")
	}
	again := NewClient(out, cfg, false)
	again.FireworksURL = s.URL
	again.Chat("j/1", "m", payload, 5)
	if calls.Load() != 1 {
		t.Fatal("cache billed twice")
	}
	defer func() {
		if recover() == nil {
			t.Error("changed payload accepted")
		}
	}()
	again.Chat("j/1", "m", []any{message("user", "changed")}, 5)
}
func TestConcurrentHardRequestCap(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-only")
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte(`{}`)) }))
	defer s.Close()
	c := NewClient(t.TempDir(), M{"timeout_s": 2, "max_requests": 2}, false)
	c.FireworksURL = s.URL
	var wg sync.WaitGroup
	for _, k := range []string{"1", "2", "3", "4"} {
		wg.Add(1)
		go func(k string) { defer wg.Done(); defer func() { recover() }(); c.Request(k, "m", M{}) }(k)
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}
func TestPublishedMetricsQuirks(t *testing.T) {
	if judgePair("1", []any{"1", "2"}) != 0 || judgePair("1", []any{"1", nil}) != 1 {
		t.Fatal("JudgeBench")
	}
	if num(llmbarPair("1", ratingPair([]any{nil, nil}))["correct_average"]) != .5 {
		t.Fatal("LLMBar error credit")
	}
	if rewardRating([]any{nil, nil}) != .25 || rewardRating([]any{3.0, 3.0, 1.0}) != .5 {
		t.Fatal("RB ties")
	}
	rows := []M{{"case_id": "ref:1", "num_correct": 1, "scores": []any{10.0, 1.0}}, {"case_id": "tied:1", "num_correct": 2, "scores": []any{10.0, 10.0, 1.0}}}
	if math.Abs(num(TiesScore(rows)["score"])-1.01) > 1e-12 {
		t.Fatal("Ties perfect")
	}
}
func TestTemplateSubstitutionDoesNotInterpretCandidate(t *testing.T) {
	if format("{input}:{output}", M{"input": "{output}", "output": "x"}, false) != "{output}:x" {
		t.Fatal("recursive substitution")
	}
}
func TestOfficialDataAndGoReplay(t *testing.T) {
	t.Skip("historical receipt integration excluded from source-free distribution")
	root := filepath.Join("..", "..")
	out := filepath.Join(root, "results/methodology-validation-v1")
	if _, e := os.Stat(filepath.Join(root, "methodology/.cache/rewardbench2_data/test.parquet")); e != nil {
		t.Skip("run prepare for official-data/replay integration test")
	}
	for b, n := range map[string]int{"llmbar": 419, "judgebench": 620, "rewardbench2": 1865} {
		if len(Load(root, b)) != n {
			t.Fatal(b)
		}
	}
	cfg := obj(ReadJSON(filepath.Join(root, "methodology/configs/validation.json")))
	if len(Plan(root, cfg)) != 525 {
		t.Fatal("sample plan")
	}
	cfg["per_subset"] = 0
	if len(Plan(root, cfg)) != 56275 {
		t.Fatal("full plan")
	}
	if _, e := os.Stat(filepath.Join(out, "results.jsonl")); e != nil {
		t.Skip("archived run absent")
	}
	manifest := obj(ReadJSON(filepath.Join(out, "manifest.json")))
	c := NewClient(out, obj(manifest["config"]), true)
	jobs := map[string]M{}
	for _, j := range arr(ReadJSON(filepath.Join(out, "jobs.json"))) {
		jobs[str(obj(j)["key"])] = obj(j)
	}
	old := Lines(filepath.Join(out, "results.jsonl"))
	for _, r := range old {
		got, e := Evaluate(root, jobs[str(r["key"])], c)
		if e != nil {
			t.Fatal(e)
		}
		if !Equivalent(decode(Canon(got)), decode(Canon(r))) {
			t.Fatal("replay", r["key"])
		}
	}
	summary := obj(ReadJSON(filepath.Join(out, "summary.json")))
	if !Equivalent(decode(Canon(Scores(old))), summary["scores"]) {
		t.Fatal("aggregate scoring differs from Python")
	}
}
