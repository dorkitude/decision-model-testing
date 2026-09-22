package study

// AtomicDiagnostics exposes rubric saturation and ties without assuming that
// the pairwise gold preference supplies ground truth for each yes/no criterion.
func AtomicDiagnostics(records []M) []M {
	groups := map[string][]M{}
	for _, r := range records {
		if r["method"] == "Atomic" {
			groups[s(r["model"])] = append(groups[s(r["model"])], r)
		}
	}
	out := []M{}
	for _, model := range orderedKeys(groups) {
		jobs, validOrders, ties, allPass, inconsistent, validJobs := len(groups[model]), 0, 0, 0, 0, 0
		for _, r := range groups[model] {
			if b(r["valid"]) {
				validJobs++
				ds := a(r["decisions"])
				if len(ds) == 2 && ds[0] != ds[1] {
					inconsistent++
				}
			}
			for _, v := range a(r["native_orders"]) {
				order := m(v)
				if !b(order["valid"]) {
					continue
				}
				validOrders++
				if order["decision"] == "TIE" {
					ties++
				}
				answers := m(order["answers"])
				all := len(answers) == 6
				for _, answer := range answers {
					all = all && b(answer)
				}
				if all {
					allPass++
				}
			}
		}
		r := M{"model": model, "jobs": jobs, "valid_jobs": validJobs, "valid_orders": validOrders, "tied_valid_orders": ties, "both_candidates_pass_all_valid_orders": allPass, "position_inconsistent_valid_jobs": inconsistent, "tied_valid_order_rate": nil, "scope": "descriptive per-order rubric saturation; two orders are not independent samples; pairwise gold does not label individual criteria"}
		if validOrders > 0 {
			r["tied_valid_order_rate"] = float64(ties) / float64(validOrders)
		}
		out = append(out, r)
	}
	return out
}
