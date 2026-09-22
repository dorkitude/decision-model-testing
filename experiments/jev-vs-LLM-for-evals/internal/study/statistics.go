package study

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
)

type Unit struct {
	Benchmark, Method, Model, Subset, ID string
	Score, Strict, Cost, KnownCost       float64
	Valid                                bool
	Rows                                 []M
}

func Units(records []M) []Unit {
	groups := map[string][]M{}
	for _, r := range records {
		id := s(r["case_id"])
		if r["benchmark"] == "rewardbench2" && r["subset"] == "Ties" {
			_, id, _ = strings.Cut(id, ":")
		}
		key := strings.Join([]string{s(r["benchmark"]), s(r["method"]), s(r["model"]), s(r["subset"]), id}, "\t")
		groups[key] = append(groups[key], r)
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []Unit{}
	for _, key := range keys {
		rs := groups[key]
		sort.Slice(rs, func(i, j int) bool { return s(rs[i]["case_id"]) < s(rs[j]["case_id"]) })
		first := rs[0]
		u := Unit{Benchmark: s(first["benchmark"]), Method: s(first["method"]), Model: s(first["model"]), Subset: s(first["subset"]), ID: s(first["case_id"]), Rows: rs, Valid: true}
		if u.Benchmark == "rewardbench2" && u.Subset == "Ties" {
			_, u.ID, _ = strings.Cut(u.ID, ":")
			if len(rs) != 2 {
				panic("incomplete or duplicate tie cohort")
			}
			u.Score = n(eval.TiesScore(rs)["score"])
		} else {
			if len(rs) != 1 {
				panic("duplicate comparison unit")
			}
			u.Score = n(first["published_score"])
		}
		for _, r := range rs {
			u.Valid = u.Valid && b(r["valid"])
			u.Cost += n(r["accounting_usd"])
			u.KnownCost += n(r["known_total_usd"])
		}
		if u.Valid {
			u.Strict = u.Score
		}
		out = append(out, u)
	}
	return out
}
func quantile(xs []float64, p float64) any {
	if len(xs) == 0 {
		return nil
	}
	ys := append([]float64{}, xs...)
	sort.Float64s(ys)
	x := p * float64(len(ys)-1)
	lo, hi := int(math.Floor(x)), int(math.Ceil(x))
	return ys[lo] + (ys[hi]-ys[lo])*(x-float64(lo))
}
func average(xs []float64) float64 {
	v := 0.0
	for _, x := range xs {
		v += x
	}
	if len(xs) == 0 {
		return 0
	}
	return v / float64(len(xs))
}
func orderedKeys[T any](m map[string]T) []string {
	keys := []string{}
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func StrictSummaries(records []M) []M {
	byGroup := map[string][]Unit{}
	for _, u := range Units(records) {
		key := strings.Join([]string{u.Benchmark, u.Method, u.Model}, "\t")
		byGroup[key] = append(byGroup[key], u)
	}
	out := []M{}
	for _, key := range orderedKeys(byGroup) {
		us := byGroup[key]
		first := us[0]
		strata := map[string][]Unit{}
		cost, known, unknown, jobs, valid := 0.0, 0.0, 0.0, 0, 0
		input, output, helper := 0.0, 0.0, 0.0
		inputUSD, outputUSD, cached, reasoning := 0., 0., 0., 0.
		wall, requestLatencies := []float64{}, []float64{}
		consistent, decisionPairs := 0, 0
		for _, u := range us {
			strata[u.Subset] = append(strata[u.Subset], u)
			cost += u.Cost
			known += u.KnownCost
			for _, r := range u.Rows {
				jobs++
				if b(r["valid"]) {
					valid++
				}
				unknown += n(r["unknown_cost_attempts"])
				input += n(r["input_tokens"])
				output += n(r["output_tokens"])
				inputUSD += n(r["known_input_usd"])
				outputUSD += n(r["known_output_usd"])
				cached += n(r["cached_input_tokens"])
				reasoning += n(r["reasoning_tokens"])
				helper += n(r["helper_accounting_usd"])
				wall = append(wall, n(r["native_wall_seconds"]))
				for _, v := range a(r["request_latencies"]) {
					requestLatencies = append(requestLatencies, n(v))
				}
				if ds := a(r["decisions"]); len(ds) == 2 {
					decisionPairs++
					if ds[0] != nil && ds[1] != nil && ds[0] == ds[1] {
						consistent++
					}
				}
			}
		}
		strict := 0.0
		subsets := M{}
		for _, subset := range orderedKeys(strata) {
			group := strata[subset]
			values := []float64{}
			for _, u := range group {
				values = append(values, u.Strict)
			}
			score := average(values)
			subsets[subset] = score
			weight := 1 / float64(len(strata))
			if first.Benchmark == "judgebench" {
				weight = float64(len(group)) / float64(len(us))
			}
			strict += weight * score
		}
		var total any
		if unknown == 0 {
			total = known
		}
		var agreement any
		if decisionPairs > 0 {
			agreement = float64(consistent) / float64(decisionPairs)
		}
		out = append(out, M{"benchmark": first.Benchmark, "method": first.Method, "model": first.Model, "units": len(us), "jobs": jobs, "valid_jobs": valid, "strict_score": strict, "aggregation_scope": "observed subsets only; partial campaigns are not full benchmark estimates", "strict_subsets": subsets, "valid_order_agreement": agreement, "estimated_total_usd": total, "known_total_usd": known, "accounting_usd": cost, "unknown_cost_attempts": unknown, "accounting_usd_per_job": cost / float64(jobs), "input_tokens": input, "output_tokens": output, "input_tokens_per_job": input / float64(jobs), "output_tokens_per_job": output / float64(jobs), "known_input_usd": inputUSD, "known_output_usd": outputUSD, "retained_unknown_cost_usd": cost - known, "cached_input_tokens": cached, "reasoning_tokens": reasoning, "helper_accounting_usd": helper, "native_wall_p50_s": quantile(wall, .5), "native_wall_p95_s": quantile(wall, .95), "request_p50_s": quantile(requestLatencies, .5), "request_p95_s": quantile(requestLatencies, .95)})
	}
	return out
}

type paired struct{ A, B Unit }

func seedFor(key string) int64 {
	h := sha256.Sum256([]byte("paired-bootstrap-v1/" + key))
	return int64(binary.BigEndian.Uint64(h[:8]))
}
func PairComparison(left, right []Unit, replicates int) M {
	if replicates < 1 {
		panic("bootstrap replicates must be positive")
	}
	lookup := map[string]Unit{}
	for _, u := range right {
		lookup[u.Subset+"\t"+u.ID] = u
	}
	groups := map[string][]paired{}
	matched := 0
	for _, u := range left {
		if other, ok := lookup[u.Subset+"\t"+u.ID]; ok {
			groups[u.Subset] = append(groups[u.Subset], paired{u, other})
			matched++
		}
	}
	if matched == 0 {
		return M{"matched_units": 0}
	}
	subsets := orderedKeys(groups)
	benchmark := left[0].Benchmark
	weights := map[string]float64{}
	for _, subset := range subsets {
		weights[subset] = 1 / float64(len(subsets))
		if benchmark == "judgebench" {
			weights[subset] = float64(len(groups[subset])) / float64(matched)
		}
	}
	summarize := func(sample bool, rng *rand.Rand) (delta, strict, ratio float64) {
		ca, cb := 0.0, 0.0
		for _, subset := range subsets {
			pairs := groups[subset]
			d, sd, costa, costb := 0.0, 0.0, 0.0, 0.0
			for i := range pairs {
				j := i
				if sample {
					j = rng.Intn(len(pairs))
				}
				p := pairs[j]
				d += p.A.Score - p.B.Score
				sd += p.A.Strict - p.B.Strict
				costa += p.A.Cost
				costb += p.B.Cost
			}
			delta += weights[subset] * d / float64(len(pairs))
			strict += weights[subset] * sd / float64(len(pairs))
			ca += costa
			cb += costb
		}
		if cb > 0 {
			ratio = ca / cb
		} else {
			ratio = math.NaN()
		}
		return
	}
	delta, strict, ratio := summarize(false, nil)
	rng := rand.New(rand.NewSource(seedFor(benchmark + left[0].Method + right[0].Model)))
	ds, ss, ratios := []float64{}, []float64{}, []float64{}
	for i := 0; i < replicates; i++ {
		d, st, r := summarize(true, rng)
		ds = append(ds, d)
		ss = append(ss, st)
		if !math.IsNaN(r) {
			ratios = append(ratios, r)
		}
	}
	var costRatio any
	if !math.IsNaN(ratio) {
		costRatio = ratio
	}
	return M{"benchmark": benchmark, "method": left[0].Method, "model_a": left[0].Model, "model_b": right[0].Model, "matched_units": matched, "left_units": len(left), "right_units": len(right), "published_delta": delta, "published_delta_ci95": []any{quantile(ds, .025), quantile(ds, .975)}, "strict_delta": strict, "strict_delta_ci95": []any{quantile(ss, .025), quantile(ss, .975)}, "accounting_cost_ratio_a_over_b": costRatio, "accounting_cost_ratio_ci95": []any{quantile(ratios, .025), quantile(ratios, .975)}, "bootstrap_replicates": replicates, "strata": subsets}
}
func PairedComparisons(records []M, replicates int) []M {
	groups := map[string]map[string][]Unit{}
	for _, u := range Units(records) {
		key := u.Benchmark + "\t" + u.Method
		if groups[key] == nil {
			groups[key] = map[string][]Unit{}
		}
		groups[key][u.Model] = append(groups[key][u.Model], u)
	}
	out := []M{}
	for _, key := range orderedKeys(groups) {
		models := groups[key]
		jev := models["jev-latest"]
		if len(jev) == 0 {
			continue
		}
		for _, model := range orderedKeys(models) {
			if model != "jev-latest" {
				out = append(out, PairComparison(jev, models[model], replicates))
			}
		}
	}
	return out
}
