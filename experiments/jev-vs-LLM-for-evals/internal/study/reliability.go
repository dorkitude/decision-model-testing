package study

import "strings"

// Reliability reports all-attempt service failures and jointly valid matched
// quality. The latter is a selection-biased diagnostic, not a replacement for
// the complete end-to-end benchmark population.
func Reliability(records []M, replicates int) M {
	groups := map[string][]Unit{}
	for _, u := range Units(records) {
		key := strings.Join([]string{u.Benchmark, u.Method, u.Model}, "\t")
		groups[key] = append(groups[key], u)
	}
	summary := []M{}
	for _, key := range orderedKeys(groups) {
		us := groups[key]
		counts := M{}
		jobs, invalid, attempts, truncated := 0, 0, 0., 0.
		for _, u := range us {
			for _, r := range u.Rows {
				jobs++
				if !b(r["valid"]) {
					invalid++
				}
				attempts += n(r["request_attempts"])
				truncated += n(r["truncated_replies"])
				for category, value := range m(r["error_attempts"]) {
					plus(counts, category, n(value))
				}
			}
		}
		summary = append(summary, M{"benchmark": us[0].Benchmark, "method": us[0].Method, "model": us[0].Model, "jobs": jobs, "invalid_jobs": invalid, "attempts": attempts, "error_attempts": counts, "truncated_replies": truncated})
	}
	comparisons := []M{}
	for _, key := range orderedKeys(groups) {
		us := groups[key]
		if us[0].Model != "jev-latest" {
			continue
		}
		for _, other := range orderedKeys(groups) {
			fs := groups[other]
			if fs[0].Benchmark != us[0].Benchmark || fs[0].Method != us[0].Method || fs[0].Model == "jev-latest" {
				continue
			}
			lookup := map[string]Unit{}
			for _, u := range fs {
				lookup[u.Subset+"\t"+u.ID] = u
			}
			left, right := []Unit{}, []Unit{}
			matched := 0
			for _, u := range us {
				f, ok := lookup[u.Subset+"\t"+u.ID]
				if !ok {
					continue
				}
				matched++
				if u.Valid && f.Valid {
					left = append(left, u)
					right = append(right, f)
				}
			}
			result := M{"benchmark": us[0].Benchmark, "method": us[0].Method, "model_a": "jev-latest", "model_b": fs[0].Model, "matched_units": matched, "jointly_valid_units": len(left), "coverage": nil, "paired": nil, "scope": "jointly valid subset only; selection may favor easier cases; not the full-population benchmark"}
			if matched > 0 {
				result["coverage"] = float64(len(left)) / float64(matched)
			}
			if len(left) > 0 {
				result["paired"] = PairComparison(left, right, replicates)
			}
			comparisons = append(comparisons, result)
		}
	}
	return M{"all_attempt_reliability": summary, "jointly_valid_comparisons": comparisons}
}
