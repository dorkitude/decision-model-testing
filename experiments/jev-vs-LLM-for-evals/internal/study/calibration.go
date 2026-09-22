package study

import "math"

// Calibration uses only the first presentation of the two preregistered
// binary Choice methods. It does not treat the reversed presentation as
// another independent observation or infer confidence from rating entropy.
func Calibration(records []M) []M {
	groups := map[string][]M{}
	for _, r := range records {
		if r["model"] != "jev-latest" {
			continue
		}
		if !(r["benchmark"] == "llmbar" && r["method"] == "Vanilla") && !(r["benchmark"] == "judgebench" && r["method"] == "vanilla") {
			continue
		}
		key := s(r["benchmark"]) + "/" + s(r["method"])
		groups[key] = append(groups[key], r)
	}
	out := []M{}
	for _, key := range orderedKeys(groups) {
		rs := groups[key]
		counts := [10]int{}
		probabilities, outcomes := [10]float64{}, [10]float64{}
		eligible, brier := 0, 0.
		for _, r := range rs {
			ps := a(r["choice_p_original_a"])
			if !b(r["valid"]) || len(ps) != 2 || ps[0] == nil {
				continue
			}
			label := s(r["label"])
			// Labels may arrive from JSON as numbers.
			if label != "1" && label != "2" {
				panic("invalid binary gold label")
			}
			p := n(ps[0])
			if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
				panic("invalid calibration probability")
			}
			y := 0.
			if label == "1" {
				y = 1
			}
			bin := int(p * 10)
			if bin == 10 {
				bin = 9
			}
			counts[bin]++
			probabilities[bin] += p
			outcomes[bin] += y
			brier += (p - y) * (p - y)
			eligible++
		}
		bins := []M{}
		ece := 0.
		for i, count := range counts {
			bin := M{"lower": float64(i) / 10, "upper": float64(i+1) / 10, "n": count, "mean_probability": nil, "observed_a_frequency": nil}
			if count > 0 {
				p, y := probabilities[i]/float64(count), outcomes[i]/float64(count)
				bin["mean_probability"] = p
				bin["observed_a_frequency"] = y
				ece += float64(count) * math.Abs(p-y)
			}
			bins = append(bins, bin)
		}
		var bs, ce any
		if eligible > 0 {
			bs = brier / float64(eligible)
			ce = ece / float64(eligible)
		}
		out = append(out, M{"benchmark": rs[0]["benchmark"], "method": rs[0]["method"], "model": "jev-latest", "jobs": len(rs), "eligible": eligible, "coverage": float64(eligible) / float64(len(rs)), "binary_brier": bs, "equal_width_ece": ce, "bins": bins, "scope": "first presentation; probability of original candidate A being gold; invalid jobs excluded"})
	}
	return out
}
