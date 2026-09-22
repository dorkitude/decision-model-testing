package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"math"
	"path/filepath"
	"testing"
)

func TestPreflightCostAndAttemptConservation(t *testing.T) {
	t.Skip("integration requires upstream data or private historical fixtures; excluded from source-free offline suite")
	dir := filepath.Join("..", "..", "results", "robust-preflight-v1")
	rows, _, e := LoadCampaign(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 60 {
		t.Fatal(len(rows))
	}
	cost, attempts := 0.0, 0.0
	for _, r := range rows {
		cost += n(r["accounting_usd"])
		attempts += n(r["request_attempts"]) + n(r["orphan_reservations"])
		if r["label"] != nil && r["fold"] == "" {
			t.Fatal("missing fold")
		}
	}
	budget := m(m(eval.ReadJSON(filepath.Join(dir, "progress.json")))["budget"])
	if math.Abs(cost-n(budget["committed_estimated_usd"])) > 1e-10 || attempts != n(budget["reserved_attempts"]) {
		t.Fatalf("cost %g attempts %g vs %v", cost, attempts, budget)
	}
}
func TestTieCompanionsShareDevelopmentFold(t *testing.T) {
	for _, id := range []string{"1", "100", "55"} {
		if Fold("rewardbench2", "Ties", "ref:"+id) != Fold("rewardbench2", "Ties", "tied:"+id) {
			t.Fatal(id)
		}
	}
}

func TestSummaryCostComponentsConserveRecordedUsage(t *testing.T) {
	t.Skip("integration requires upstream data or private historical fixtures; excluded from source-free offline suite")
	rows, _, err := LoadCampaign(filepath.Join("..", "..", "results", "robust-preflight-v1"))
	if err != nil {
		t.Fatal(err)
	}
	total := 0.
	for _, r := range StrictSummaries(rows) {
		if math.Abs(n(r["known_input_usd"])+n(r["known_output_usd"])-n(r["known_total_usd"])) > 1e-10 {
			t.Fatal("input/output charges do not sum", r)
		}
		if math.Abs(n(r["known_total_usd"])+n(r["retained_unknown_cost_usd"])-n(r["accounting_usd"])) > 1e-10 {
			t.Fatal("unknown reservation accounting lost", r)
		}
		total += n(r["accounting_usd"])
	}
	want := 0.
	for _, r := range rows {
		want += n(r["accounting_usd"])
	}
	if math.Abs(total-want) > 1e-10 {
		t.Fatal("summary cost not conserved")
	}
}

func TestRepeatedLoadsHaveIdenticalProvenance(t *testing.T) {
	t.Skip("integration requires upstream data or private historical fixtures; excluded from source-free offline suite")
	dir := filepath.Join("..", "..", "results", "robust-preflight-v1")
	var want string
	for i := 0; i < 12; i++ {
		rows, _, err := LoadCampaign(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := eval.Hash(eval.Canon(rows))
		if i == 0 {
			want = got
		} else if got != want {
			t.Fatalf("immutable receipts produced different record hashes: %s vs %s", want, got)
		}
	}
}
