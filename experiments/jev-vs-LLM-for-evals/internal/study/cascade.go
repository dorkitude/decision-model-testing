package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"math"
	"strings"
)

var cascadeFallbacks = []string{"accounts/fireworks/models/gpt-oss-120b", "accounts/fireworks/models/qwen3p8-max"}

func cascadeGroups(records []M) map[string][]paired {
	byKey := map[string]map[string]Unit{}
	for _, u := range Units(records) {
		if !(u.Benchmark == "llmbar" && u.Method == "Vanilla") && !(u.Benchmark == "judgebench" && u.Method == "vanilla") {
			continue
		}
		key := strings.Join([]string{u.Benchmark, u.Method, u.Subset, u.ID}, "\t")
		if byKey[key] == nil {
			byKey[key] = map[string]Unit{}
		}
		byKey[key][u.Model] = u
	}
	out := map[string][]paired{}
	for _, key := range orderedKeys(byKey) {
		models := byKey[key]
		j, ok := models["jev-latest"]
		if !ok {
			panic("cascade missing Jev comparison")
		}
		for _, model := range cascadeFallbacks {
			f, ok := models[model]
			if !ok {
				panic("cascade missing fallback comparison")
			}
			group := strings.Join([]string{j.Benchmark, j.Method, model}, "\t")
			out[group] = append(out[group], paired{j, f})
		}
	}
	return out
}
func cascadeAccept(u Unit, threshold float64) bool {
	if !u.Valid {
		return false
	}
	r := u.Rows[0]
	ds, ps := a(r["decisions"]), a(r["choice_confidence"])
	if len(ds) != 2 || ds[0] == nil || ds[0] != ds[1] || len(ps) != 2 || ps[0] == nil || ps[1] == nil {
		return false
	}
	for _, p := range ps {
		v := n(p)
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return false
		}
	}
	return math.Min(n(ps[0]), n(ps[1])) >= threshold
}
func cascadeRows(pairs []paired, threshold float64) ([]M, []Unit, int) {
	rows := []M{}
	fallback := []Unit{}
	accepted := 0
	for _, p := range pairs {
		accept := cascadeAccept(p.A, threshold)
		chosen := p.B
		if accept {
			chosen = p.A
			accepted++
		}
		r := M{}
		for k, v := range chosen.Rows[0] {
			r[k] = v
		}
		r["model"] = "jev-first/" + p.B.Model
		r["cascade_accepted"] = accept
		// Always pay for Jev, even when its verdict is invalid or inconsistent.
		for _, field := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens", "known_input_usd", "known_output_usd", "known_total_usd", "accounting_usd", "unknown_cost_attempts", "request_attempts", "transport_failures", "truncated_replies", "helper_attempts", "helper_accounting_usd", "native_wall_seconds", "request_seconds"} {
			value := n(p.A.Rows[0][field])
			if !accept {
				value += n(p.B.Rows[0][field])
			}
			r[field] = value
		}
		ls := append([]any{}, a(p.A.Rows[0]["request_latencies"])...)
		if !accept {
			ls = append(ls, a(p.B.Rows[0]["request_latencies"])...)
		}
		r["request_latencies"] = ls
		rows = append(rows, r)
		fallback = append(fallback, p.B)
	}
	return rows, fallback, accepted
}
func cascadeMean(us []Unit, strict bool) float64 {
	if len(us) == 0 {
		panic("empty cascade fold")
	}
	subsets := map[string][]float64{}
	for _, u := range us {
		v := u.Score
		if strict {
			v = u.Strict
		}
		subsets[u.Subset] = append(subsets[u.Subset], v)
	}
	total := 0.
	for _, subset := range orderedKeys(subsets) {
		vs := subsets[subset]
		weight := 1 / float64(len(subsets))
		if us[0].Benchmark == "judgebench" {
			weight = float64(len(vs)) / float64(len(us))
		}
		total += weight * average(vs)
	}
	return total
}
func cascadeSplit(pairs []paired, fold string) []paired {
	out := []paired{}
	for _, p := range pairs {
		if Fold(p.A.Benchmark, p.A.Subset, p.A.ID) == fold {
			out = append(out, p)
		}
	}
	return out
}

// SelectCascades must only be called on complete campaign data. Its output
// must be persisted before EvaluateCascades is called in a separate command.
func SelectCascades(records []M, complete bool) M {
	if !complete {
		panic("cascade selection requires a complete campaign")
	}
	selections := M{}
	groups := cascadeGroups(records)
	for _, key := range orderedKeys(groups) {
		dev := cascadeSplit(groups[key], "development")
		if len(dev) == 0 {
			panic("empty development fold")
		}
		fallback := []Unit{}
		for _, p := range dev {
			fallback = append(fallback, p.B)
		}
		target := cascadeMean(fallback, true) - .01
		thresholds := []float64{0}
		for i := 50; i <= 101; i++ {
			thresholds = append(thresholds, float64(i)/100)
		}
		best, bestCost := 1.01, math.Inf(1)
		curve := []M{}
		for _, threshold := range thresholds {
			rows, _, accepted := cascadeRows(dev, threshold)
			us := Units(rows)
			score := cascadeMean(us, true)
			cost := 0.
			for _, u := range us {
				cost += u.Cost
			}
			feasible := score+1e-12 >= target
			curve = append(curve, M{"threshold": threshold, "strict_score": score, "accounting_usd": cost, "accepted": accepted, "feasible": feasible})
			if feasible && (cost < bestCost-1e-12 || (math.Abs(cost-bestCost) <= 1e-12 && threshold > best)) {
				best, bestCost = threshold, cost
			}
		}
		selections[key] = M{"threshold": best, "development_units": len(dev), "fallback_strict_score": target + .01, "development_curve": curve}
	}
	if len(selections) != 4 {
		panic("expected four preregistered cascade comparisons")
	}
	return M{"protocol": "cascade-development-v1", "source_records_sha256": eval.Hash(eval.Canon(records)), "selections": selections, "scope": "thresholds selected on development only; held-out outcomes not evaluated"}
}
func EvaluateCascades(records []M, complete bool, frozen M, replicates int) []M {
	if !complete || frozen["protocol"] != "cascade-development-v1" || frozen["source_records_sha256"] != eval.Hash(eval.Canon(records)) {
		panic("cascade selection/data provenance mismatch or incomplete campaign")
	}
	groups := cascadeGroups(records)
	selections := m(frozen["selections"])
	if len(selections) != len(groups) {
		panic("cascade selection group mismatch")
	}
	out := []M{}
	for _, key := range orderedKeys(groups) {
		selection := m(selections[key])
		if selection["threshold"] == nil {
			panic("missing frozen threshold")
		}
		held := cascadeSplit(groups[key], "held_out")
		if len(held) == 0 {
			panic("empty held-out fold")
		}
		threshold := n(selection["threshold"])
		rows, fallback, accepted := cascadeRows(held, threshold)
		us := Units(rows)
		novel := unseenQuestionPairs(held, cascadeSplit(groups[key], "development"))
		sensitivity := M{"eligible_units": len(novel), "excluded_units": len(held) - len(novel), "scope": "same frozen threshold; excludes exact question texts seen in development; does not remove within-held-out duplicates or semantic/training contamination"}
		if len(novel) > 0 {
			nr, nf, na := cascadeRows(novel, threshold)
			nu := Units(nr)
			sensitivity["acceptance_coverage"] = float64(na) / float64(len(novel))
			sensitivity["strict_score"] = cascadeMean(nu, true)
			sensitivity["published_score"] = cascadeMean(nu, false)
			sensitivity["paired"] = PairComparison(nu, nf, replicates)
			sensitivity["resources"] = StrictSummaries(nr)
		}
		out = append(out, M{"comparison": key, "unseen_exact_question_sensitivity": sensitivity, "threshold": threshold, "held_out_units": len(held), "accepted": accepted, "acceptance_coverage": float64(accepted) / float64(len(held)), "published_score": cascadeMean(us, false), "strict_score": cascadeMean(us, true), "fallback_published_score": cascadeMean(fallback, false), "fallback_strict_score": cascadeMean(fallback, true), "paired": PairComparison(us, fallback, replicates), "resources": StrictSummaries(rows), "scope": "offline cascade; both-order cost paid; sequential latency estimated from independent jobs"})
	}
	return out
}
