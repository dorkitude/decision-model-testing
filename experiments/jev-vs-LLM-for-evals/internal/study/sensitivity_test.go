package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"testing"
)

func TestExpectedSensitivityKeepsPrimaryAndCost(t *testing.T) {
	r := M{"benchmark": "llmbar", "method": "Rating", "model": "jev-latest", "subset": "Natural", "case_id": "a", "label": "1", "valid": true, "scores": []any{8., 8.}, "expected_scores": []any{8.4, 8.1}, "published_score": .5, "accounting_usd": .01, "known_total_usd": .01}
	before := eval.Hash(eval.Canon(r))
	got := RatingSensitivity([]M{r}, 20)
	pair := got["paired_expected_minus_modal"].([]M)[0]
	if n(pair["published_delta"]) != .5 || n(pair["accounting_cost_ratio_a_over_b"]) != 1 {
		t.Fatal(pair)
	}
	if before != eval.Hash(eval.Canon(r)) {
		t.Fatal("primary record mutated")
	}
	r["expected_scores"] = []any{nil, 8.1}
	got = RatingSensitivity([]M{r}, 20)
	if n(got["expected_scores_available"]) != 1 || n(got["resources_and_strict_scores"].([]M)[0]["valid_jobs"]) != 0 {
		t.Fatal(got)
	}
}
