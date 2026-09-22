package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"math"
	"reflect"
	"testing"
)

func TestTieUnitsPreserveWholeCohortScore(t *testing.T) {
	rows := []M{}
	for i, pair := range [][][]any{{{8., 2.}, {8., 7., 2.}}, {{3., 5.}, {4., 4., 5.}}, {{9., 1.}, {9., 9., 1.}}} {
		for j, kind := range []string{"ref", "tied"} {
			rows = append(rows, M{"benchmark": "rewardbench2", "method": "ratings", "model": "jev-latest", "subset": "Ties", "case_id": kind + ":" + string(rune('a'+i)), "scores": pair[j], "num_correct": j + 1, "valid": i != 1, "accounting_usd": .01})
		}
	}
	units := Units(rows)
	got := 0.
	for _, u := range units {
		got += u.Score / float64(len(units))
		if !u.Valid && u.Strict != 0 {
			t.Fatal("invalid tie unit received strict credit")
		}
		if math.Abs(u.Cost-.02) > 1e-12 {
			t.Fatal("companion cost lost")
		}
	}
	want := n(eval.TiesScore(rows)["score"])
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("per-unit mean %g != whole cohort %g", got, want)
	}
}
func TestPairedBootstrapPreservesPairingAndWeights(t *testing.T) {
	left, right := []Unit{}, []Unit{}
	for i := 0; i < 4; i++ {
		subset := "large"
		score := 1.
		if i == 3 {
			subset = "small"
			score = 0
		}
		left = append(left, Unit{Benchmark: "llmbar", Method: "Vanilla", Model: "jev-latest", Subset: subset, ID: string(rune('a' + i)), Score: score, Strict: score, Cost: 1})
		right = append(right, Unit{Benchmark: "llmbar", Method: "Vanilla", Model: "fallback", Subset: subset, ID: string(rune('a' + i)), Cost: 2})
	}
	r := PairComparison(left, right, 100)
	if n(r["published_delta"]) != .5 || n(r["accounting_cost_ratio_a_over_b"]) != .5 {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(r["published_delta_ci95"], []any{.5, .5}) {
		t.Fatal("stratification/pairing lost", r)
	}
	if !reflect.DeepEqual(r, PairComparison(left, right, 100)) {
		t.Fatal("bootstrap nondeterministic")
	}
	for i := range left {
		left[i].Benchmark = "judgebench"
		right[i].Benchmark = "judgebench"
	}
	if n(PairComparison(left, right, 100)["published_delta"]) != .75 {
		t.Fatal("JudgeBench population weights lost")
	}
	right = right[:3]
	if n(PairComparison(left, right, 100)["matched_units"]) != 3 {
		t.Fatal("unmatched unit included")
	}
	for i := range right {
		right[i].Cost = 0
	}
	if PairComparison(left, right, 100)["accounting_cost_ratio_a_over_b"] != nil {
		t.Fatal("zero-cost denominator was fabricated")
	}
}
