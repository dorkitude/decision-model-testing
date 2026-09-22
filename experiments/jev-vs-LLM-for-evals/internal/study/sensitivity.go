package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"math"
	"strings"
)

func RatingSensitivity(records []M, replicates int) M {
	primary, continuous := []M{}, []M{}
	available, total := 0, 0
	for _, r := range records {
		if r["model"] != "jev-latest" || !(r["benchmark"] == "llmbar" && strings.HasPrefix(s(r["method"]), "Rating") || r["benchmark"] == "rewardbench2" && r["method"] == "ratings") {
			continue
		}
		scores := a(r["expected_scores"])
		original := a(r["scores"])
		if len(scores) != len(original) {
			panic("missing expected-rating vector")
		}
		clean := append([]any{}, scores...)
		for i, v := range clean {
			total++
			if v != nil && !math.IsNaN(n(v)) && !math.IsInf(n(v), 0) {
				available++
			} else {
				clean[i] = nil
			}
		}
		changed := eval.RescoreRatings(r, clean)
		changed["model"] = "jev-expected-score-offline"
		primary = append(primary, r)
		continuous = append(continuous, changed)
	}
	groups := map[string][]Unit{}
	for _, u := range Units(primary) {
		groups[u.Benchmark+"\t"+u.Method] = append(groups[u.Benchmark+"\t"+u.Method], u)
	}
	changedGroups := map[string][]Unit{}
	for _, u := range Units(continuous) {
		key := u.Benchmark + "\t" + u.Method
		changedGroups[key] = append(changedGroups[key], u)
	}
	pairs := []M{}
	for _, key := range orderedKeys(groups) {
		pairs = append(pairs, PairComparison(changedGroups[key], groups[key], replicates))
	}
	return M{"scope": "offline native continuous-score sensitivity versus modal primary; same API receipts and cost, no new inference; published rating tie rules retained; not an LLM discrete-scale replication", "jobs": len(primary), "expected_scores_available": available, "expected_scores_total": total, "scores": eval.Scores(continuous), "resources_and_strict_scores": StrictSummaries(continuous), "paired_expected_minus_modal": pairs}
}
