package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

func truth(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func mean(v []float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}
func llmbarPair(label any, d []any) M {
	a, b := d[0], d[1]
	return M{"correct_False": truth(a == label), "correct_True": truth(b == label), "correct_average": (truth(a == label) + truth(b == label)) / 2, "correct_both": truth(a == label && b == label), "equal": truth(a == b), "valid_agreement": truth(a != nil && b != nil && a == b)}
}
func judgePair(label any, d []any) float64 {
	v := 0
	for _, x := range d {
		if x == label {
			v++
		} else if x == flip(label) {
			v--
		}
	}
	return truth(v > 0)
}
func ratingPair(scores []any) []any {
	a, b := scores[0], scores[1]
	if a == nil || b == nil {
		return []any{"1", "2"}
	}
	first, second := "2", "1"
	if num(a) > num(b) {
		first = "1"
	}
	if num(b) > num(a) {
		second = "2"
	}
	return []any{first, second}
}
func rewardRating(scores []any) float64 {
	high := math.Inf(-1)
	for _, s := range scores {
		if s != nil && num(s) != -1 {
			high = math.Max(high, num(s))
		}
	}
	if math.IsInf(high, -1) {
		return .25
	}
	w := 0
	for _, s := range scores {
		if s != nil && num(s) == high {
			w++
		}
	}
	return truth(scores[0] != nil && num(scores[0]) == high) / float64(w)
}
func TiesScore(rows []M) M {
	type stat struct{ acc, spread, gap float64 }
	stats := map[string]map[string]stat{"ref": {}, "tied": {}}
	for _, r := range rows {
		k, g, ok := strings.Cut(str(r["case_id"]), ":")
		if !ok {
			k, g, ok = strings.Cut(str(r["id"]), ":")
		}
		if !ok || stats[k] == nil {
			panic("invalid tie ID")
		}
		scores := arr(r["scores"])
		n := int(num(r["num_correct"]))
		if n < 1 || n >= len(scores) {
			panic("invalid tie cohort")
		}
		lo, hi, bad := math.Inf(1), math.Inf(-1), math.Inf(-1)
		for i, s := range scores {
			v := -1.0
			if s != nil {
				v = num(s)
			}
			if i < n {
				lo = math.Min(lo, v)
				hi = math.Max(hi, v)
			} else {
				bad = math.Max(bad, v)
			}
		}
		spread := hi - lo
		if n == 1 {
			spread = math.NaN()
		}
		if _, exists := stats[k][g]; exists {
			panic("duplicate tie ID")
		}
		stats[k][g] = stat{truth(lo > bad), spread, lo - bad}
	}
	if len(stats["ref"]) == 0 || len(stats["ref"]) != len(stats["tied"]) {
		panic("missing paired ties")
	}
	parts := M{"tied_accuracy": 0.0, "ref_accuracy": 0.0, "correctness_preferred": 0.0, "correctness_preferred_hard": 0.0, "correctness_margin_score": 0.0}
	ids := []string{}
	for id := range stats["ref"] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ref := stats["ref"][id]
		tied, ok := stats["tied"][id]
		if !ok {
			panic("missing paired ties")
		}
		gap := math.Min(ref.gap, tied.gap)
		margin := math.Tanh(gap/tied.spread - 1)
		if math.IsNaN(margin) {
			margin = 0
		}
		for k, v := range map[string]float64{"tied_accuracy": tied.acc, "ref_accuracy": ref.acc, "correctness_preferred": truth(tied.gap > tied.spread), "correctness_preferred_hard": truth(gap > tied.spread), "correctness_margin_score": margin} {
			parts[k] = num(parts[k]) + v/float64(len(ids))
		}
	}
	parts["score"] = num(parts["tied_accuracy"])*.3 + num(parts["ref_accuracy"])*.3 + num(parts["correctness_preferred"])*.2 + num(parts["correctness_preferred_hard"])*.2 + num(parts["correctness_margin_score"])*.01
	return parts
}
func Scores(results []M) []M {
	groups := map[string][]M{}
	for _, r := range results {
		k := str(r["benchmark"]) + "\t" + str(r["method"]) + "\t" + str(r["model"])
		groups[k] = append(groups[k], r)
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	output := []M{}
	for _, k := range keys {
		rows := groups[k]
		b, m, model := rows[0]["benchmark"], rows[0]["method"], rows[0]["model"]
		subsets := map[string][]M{}
		valid := 0
		pooled := []float64{}
		for _, r := range rows {
			subsets[str(r["subset"])] = append(subsets[str(r["subset"])], r)
			if yes(r["valid"]) {
				valid++
			}
			pooled = append(pooled, num(r["published_score"]))
		}
		scores := M{}
		entry := M{"benchmark": b, "method": m, "model": model, "n": len(rows), "subsets": scores, "valid_jobs": valid}
		for subset, items := range subsets {
			if b == "rewardbench2" && subset == "Ties" {
				detail := TiesScore(items)
				entry["ties"] = detail
				scores[subset] = detail["score"]
			} else {
				xs := []float64{}
				for _, r := range items {
					xs = append(xs, num(r["published_score"]))
				}
				scores[subset] = mean(xs)
			}
		}
		if b == "rewardbench2" || b == "llmbar" {
			required := []string{"Factuality", "Precise IF", "Math", "Safety", "Focus", "Ties"}
			if b == "llmbar" {
				required = []string{"Natural", "Adversarial/Neighbor", "Adversarial/GPTInst", "Adversarial/GPTOut", "Adversarial/Manual"}
			}
			xs := []float64{}
			for _, s := range required {
				if v, ok := scores[s]; ok {
					xs = append(xs, num(v))
				}
			}
			entry["published_macro_score"] = nil
			if len(xs) == len(required) {
				entry["published_macro_score"] = mean(xs)
			}
		}
		if b != "rewardbench2" {
			entry["published_score"] = mean(pooled)
		}
		if b == "llmbar" {
			entry["pooled_score"] = mean(pooled)
			adv := []float64{}
			for _, s := range Keys(scores) {
				if strings.HasPrefix(s, "Adversarial/") {
					adv = append(adv, num(scores[s]))
				}
			}
			entry["adversarial_macro_score"] = nil
			if len(adv) == 4 {
				entry["adversarial_macro_score"] = mean(adv)
			}
			metrics := M{}
			for _, mk := range Keys(obj(rows[0]["metrics"])) {
				xs := []float64{}
				for _, r := range rows {
					xs = append(xs, num(obj(r["metrics"])[mk]))
				}
				metrics[mk] = mean(xs)
			}
			entry["metrics"] = metrics
		}
		if b == "judgebench" {
			splits := map[string][]float64{}
			for _, r := range rows {
				splits[str(r["split"])] = append(splits[str(r["split"])], num(r["published_score"]))
			}
			v := M{}
			for s, xs := range splits {
				v[s] = mean(xs)
			}
			entry["split_scores"] = v
		}
		output = append(output, entry)
	}
	return output
}

// JSON numeric types and floating-point reduction order may differ across languages.
func Equivalent(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, exists := y[k]
			if !exists || !Equivalent(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equivalent(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		return math.Abs(x-num(b)) <= 1e-12 && b != nil && (fmt.Sprintf("%T", b) == "float64" || fmt.Sprintf("%T", b) == "int")
	default:
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
}

// RescoreRatings applies the same published scoring rules to an alternative
// set of rating values. It is for explicitly labeled offline sensitivity
// analysis; it never mutates the primary record or changes input labels.
func RescoreRatings(record M, scores []any) M {
	r := M{}
	for k, v := range record {
		r[k] = v
	}
	r["scores"] = scores
	valid := yes(record["valid"])
	for _, v := range scores {
		if v == nil {
			valid = false
		}
	}
	r["valid"] = valid
	switch str(record["benchmark"]) {
	case "llmbar":
		if len(scores) != 2 {
			panic("LLMBar ratings require two scores")
		}
		r["decisions"] = ratingPair(scores)
		r["metrics"] = llmbarPair(record["label"], arr(r["decisions"]))
		r["published_score"] = obj(r["metrics"])["correct_average"]
	case "rewardbench2":
		if len(scores) < 2 {
			panic("RewardBench ratings require multiple scores")
		}
		if record["subset"] != "Ties" {
			r["published_score"] = rewardRating(scores)
		}
	default:
		panic("unsupported rating sensitivity benchmark")
	}
	return r
}
