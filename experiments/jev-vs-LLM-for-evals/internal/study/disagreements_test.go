package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"testing"
)

func TestDisagreementCategoriesAndDeterministicExamples(t *testing.T) {
	rows := cascadeFixture()
	for _, r := range rows {
		if r["model"] == "jev-latest" {
			r["published_score"] = 0.
		}
	}
	first := Disagreements(rows, 2)
	for _, group := range first {
		if n(m(group["counts"])["jev_lower_strict_score"]) != 60 || len(group["examples"].([]M)) != 2 {
			t.Fatal(group)
		}
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	if eval.Hash(eval.Canon(first)) != eval.Hash(eval.Canon(Disagreements(rows, 2))) {
		t.Fatal("input order changed example selection")
	}
}
