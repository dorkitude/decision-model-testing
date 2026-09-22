package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"sort"
	"strings"
)

// Disagreements retains paired units, including both Ties companions. Example
// selection is deterministic by case hash, never by narrative convenience.
func Disagreements(records []M, examplesPerCategory int) []M {
	if examplesPerCategory < 0 {
		panic("negative example limit")
	}
	groups := map[string]map[string]map[string]Unit{}
	for _, u := range Units(records) {
		group := u.Benchmark + "\t" + u.Method
		if groups[group] == nil {
			groups[group] = map[string]map[string]Unit{}
		}
		if groups[group][u.Model] == nil {
			groups[group][u.Model] = map[string]Unit{}
		}
		groups[group][u.Model][u.Subset+"\t"+u.ID] = u
	}
	out := []M{}
	for _, group := range orderedKeys(groups) {
		models := groups[group]
		jev := models["jev-latest"]
		for _, model := range orderedKeys(models) {
			if model == "jev-latest" {
				continue
			}
			buckets := map[string][]M{}
			matched := 0
			for _, id := range orderedKeys(jev) {
				j := jev[id]
				f, ok := models[model][id]
				if !ok {
					continue
				}
				matched++
				category := "equal_strict_score"
				if j.Strict > f.Strict {
					category = "jev_higher_strict_score"
				} else if j.Strict < f.Strict {
					category = "jev_lower_strict_score"
				}
				if !j.Valid || !f.Valid {
					category = "required_stage_failure/" + category
				}
				// Equal aggregate credit can hide different decisions in the two orders.
				different := false
				fallbackRows := map[string]M{}
				for _, r := range f.Rows {
					fallbackRows[s(r["case_id"])] = r
				}
				for _, jr := range j.Rows {
					fr, ok := fallbackRows[s(jr["case_id"])]
					if !ok {
						panic("mismatched disagreement companion IDs")
					}
					for _, field := range []string{"decisions", "scores", "choice"} {
						if eval.Hash(eval.Canon(jr[field])) != eval.Hash(eval.Canon(fr[field])) {
							different = true
						}
					}
				}
				if category == "equal_strict_score" && different {
					category = "equal_strict_score_different_verdicts"
				}
				example := M{"case_id": j.ID, "subset": j.Subset, "category": category, "jev_published_score": j.Score, "fallback_published_score": f.Score, "jev_strict_score": j.Strict, "fallback_strict_score": f.Strict, "jev_accounting_usd": j.Cost, "fallback_accounting_usd": f.Cost, "jev_valid": j.Valid, "fallback_valid": f.Valid, "jev_records": j.Rows, "fallback_records": f.Rows}
				buckets[category] = append(buckets[category], example)
			}
			counts := M{}
			examples := []M{}
			for _, category := range orderedKeys(buckets) {
				es := buckets[category]
				counts[category] = len(es)
				if category == "equal_strict_score" {
					continue
				}
				sort.Slice(es, func(i, j int) bool {
					rank := func(r M) string {
						return eval.Hash(eval.Canon([]any{"disagreement-example-v1", group, model, r["subset"], r["case_id"]}))
					}
					return rank(es[i]) < rank(es[j])
				})
				limit := examplesPerCategory
				if len(es) < limit {
					limit = len(es)
				}
				examples = append(examples, es[:limit]...)
			}
			parts := strings.Split(group, "\t")
			out = append(out, M{"benchmark": parts[0], "method": parts[1], "comparator": model, "matched_units": matched, "counts": counts, "examples": examples, "selection": "first fixed case hashes per category; equal-score identical verdicts excluded from examples; scores are not explanations of model reasoning"})
		}
	}
	return out
}
