package study

// NativeComparisons compares protocols within each model on exactly matched
// LLMBar cases, rejecting input/answer/label differences between campaigns.
func NativeComparisons(primary, native []M, replicates int) []M {
	groups := map[string]map[string][]Unit{}
	add := func(records []M, allowed map[string]bool) {
		for _, u := range Units(records) {
			if u.Benchmark != "llmbar" || !allowed[u.Method] {
				continue
			}
			if groups[u.Model] == nil {
				groups[u.Model] = map[string][]Unit{}
			}
			groups[u.Model][u.Method] = append(groups[u.Model][u.Method], u)
		}
	}
	add(primary, map[string]bool{"Vanilla": true})
	add(native, map[string]bool{"Compact": true, "Atomic": true})
	out := []M{}
	for _, model := range orderedKeys(groups) {
		for _, methods := range [][2]string{{"Compact", "Vanilla"}, {"Atomic", "Vanilla"}, {"Atomic", "Compact"}} {
			left, right := groups[model][methods[0]], groups[model][methods[1]]
			lookup := map[string]Unit{}
			for _, u := range right {
				key := u.Subset + "\t" + u.ID
				if _, exists := lookup[key]; exists {
					panic("duplicate native comparison case")
				}
				lookup[key] = u
			}
			ls, rs := []Unit{}, []Unit{}
			seen := map[string]bool{}
			for _, u := range left {
				key := u.Subset + "\t" + u.ID
				if seen[key] {
					panic("duplicate native comparison case")
				}
				seen[key] = true
				v, ok := lookup[key]
				if !ok {
					continue
				}
				h := u.Rows[0]["comparison_content_sha256"]
				if h == nil || h == "" || h != v.Rows[0]["comparison_content_sha256"] {
					panic("native comparison input, answers or label mismatch")
				}
				ls = append(ls, u)
				rs = append(rs, v)
			}
			r := M{"benchmark": "llmbar", "model": model, "method_a": methods[0], "method_b": methods[1], "available_a": len(left), "available_b": len(right), "matched_units": len(ls), "paired": nil, "scope": "within-model method comparison on shared observed cases only; different prompts are intentional; concurrent campaigns are not isolated latency trials"}
			if len(ls) > 0 {
				r["paired"] = PairComparison(ls, rs, replicates)
				r["published_score_a"] = cascadeMean(ls, false)
				r["published_score_b"] = cascadeMean(rs, false)
				r["strict_score_a"] = cascadeMean(ls, true)
				r["strict_score_b"] = cascadeMean(rs, true)
			}
			out = append(out, r)
		}
	}
	return out
}
