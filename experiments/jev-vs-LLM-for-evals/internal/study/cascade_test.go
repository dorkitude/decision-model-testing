package study

import (
	"fmt"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"math"
	"testing"
)

func cascadeFixture() []M {
	rows := []M{}
	for _, benchmark := range []string{"llmbar", "judgebench"} {
		method := "Vanilla"
		if benchmark == "judgebench" {
			method = "vanilla"
		}
		for i := 0; i < 60; i++ {
			id := fmt.Sprint(i)
			for _, model := range append([]string{"jev-latest"}, cascadeFallbacks...) {
				cost := 1.
				if model != "jev-latest" {
					cost = 10
				}
				rows = append(rows, M{"benchmark": benchmark, "method": method, "model": model, "subset": "fixture", "case_id": id, "question_sha256": benchmark + "/" + id, "valid": true, "published_score": 1., "decisions": []any{"1", "1"}, "choice_confidence": []any{.8, .9}, "accounting_usd": cost, "known_total_usd": cost, "input_tokens": cost * 100, "native_wall_seconds": cost, "request_latencies": []any{cost}})
			}
		}
	}
	return rows
}
func TestCascadeFrozenSelectionAndHeldOutIsolation(t *testing.T) {
	rows := cascadeFixture()
	selected := SelectCascades(rows, true)
	for _, v := range m(selected["selections"]) {
		if n(m(v)["threshold"]) != .8 {
			t.Fatal("higher threshold tie break failed", v)
		}
	}
	results := EvaluateCascades(rows, true, selected, 100)
	for _, r := range results {
		if n(r["acceptance_coverage"]) != 1 || n(r["strict_score"]) != 1 || math.Abs(n(m(r["paired"])["accounting_cost_ratio_a_over_b"])-.1) > 1e-12 {
			t.Fatal(r)
		}
	}
	// Poison held-out Jev quality: selection must not change, evaluation must.
	for _, r := range rows {
		if r["model"] == "jev-latest" && Fold(s(r["benchmark"]), s(r["subset"]), s(r["case_id"])) == "held_out" {
			r["published_score"] = 0.
		}
	}
	again := SelectCascades(rows, true)
	if eval.Hash(eval.Canon(selected["selections"])) != eval.Hash(eval.Canon(again["selections"])) {
		t.Fatal("held-out outcomes leaked into selection")
	}
	for _, r := range EvaluateCascades(rows, true, again, 100) {
		if n(r["strict_score"]) != 0 {
			t.Fatal("held-out outcomes ignored")
		}
	}
	mustPanic := func(f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Error("expected provenance/completeness refusal")
			}
		}()
		f()
	}
	mustPanic(func() { EvaluateCascades(rows, true, selected, 100) })
	mustPanic(func() { SelectCascades(rows, false) })
}
func TestCascadeDeferralPaysBothModels(t *testing.T) {
	rows := cascadeFixture()
	groups := cascadeGroups(rows)
	pair := groups[orderedKeys(groups)[0]][0]
	pair.A.Rows[0]["decisions"] = []any{"1", "2"}
	got, _, accepted := cascadeRows([]paired{pair}, 0)
	if accepted != 0 || n(got[0]["accounting_usd"]) != 11 || n(got[0]["input_tokens"]) != 1100 || n(got[0]["native_wall_seconds"]) != 11 {
		t.Fatal(got)
	}
	pair.A.Rows[0]["decisions"] = []any{"1", "1"}
	pair.A.Rows[0]["choice_confidence"] = []any{nil, .9}
	if cascadeAccept(pair.A, 0) {
		t.Fatal("missing confidence accepted")
	}
	pair.A.Valid = false
	pair.A.Rows[0]["choice_confidence"] = []any{1., 1.}
	if cascadeAccept(pair.A, 0) {
		t.Fatal("invalid output accepted")
	}
}
