package main

import (
	"encoding/json"
	"fmt"

	"math/big"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

var costModels = map[string]string{"answer": "kimi-k3", "filter": "jev-1.13.0", "rerank": "qwen3-reranker-8b", "embedding": "qwen3-embedding-8b"}
var costPhases = []string{"answer", "filter", "rerank", "embedding"}
var costArms = []string{"baseline", "jev_filtered"}
var costScenarios = []string{"primary", "no_cache", "qwen_default_cache"}

func decimal(r *big.Rat) string { // Monetary arithmetic stays rational; token rates are terminating decimals.
	s := r.FloatString(30)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return s
}
func priceTokens(input, cached, output int64, ri, rc, ro *big.Rat) *big.Rat {
	v := new(big.Rat).Mul(big.NewRat(input-cached, 1), ri)
	v.Add(v, new(big.Rat).Mul(big.NewRat(cached, 1), rc))
	v.Add(v, new(big.Rat).Mul(big.NewRat(output, 1), ro))
	return v.Quo(v, big.NewRat(1000000, 1))
}
func tokenUsage(r M) (M, error) {
	u, ok := obj(r["response"])["usage"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing usage: %v", r["id"])
	}
	values := M{}
	for _, pair := range [][2]string{{"input_tokens", "prompt_tokens"}, {"output_tokens", "completion_tokens"}} {
		v, ok := u[pair[1]]
		if !ok {
			v, ok = u[pair[0]]
		}
		n, isNumber := v.(float64)
		if !ok || !isNumber || n < 0 || n != float64(int64(n)) {
			return nil, fmt.Errorf("invalid %s usage: %v", pair[0], r["id"])
		}
		values[pair[0]] = n
	}
	for field, pair := range map[string][2]string{"cached_input_tokens": {"prompt_tokens_details", "cached_tokens"}, "reasoning_tokens": {"completion_tokens_details", "reasoning_tokens"}} {
		n := float64(0)
		if details, present := u[pair[0]]; present {
			d, ok := details.(map[string]any)
			if !ok && details != nil {
				return nil, fmt.Errorf("invalid %s details", field)
			}
			if value, present := d[pair[1]]; present {
				var ok bool
				n, ok = value.(float64)
				if !ok {
					return nil, fmt.Errorf("invalid %s", field)
				}
			}
		}
		if n < 0 || n != float64(int64(n)) {
			return nil, fmt.Errorf("invalid %s", field)
		}
		values[field] = n
	}
	if num(values["cached_input_tokens"]) > num(values["input_tokens"]) || num(values["reasoning_tokens"]) > num(values["output_tokens"]) {
		return nil, fmt.Errorf("invalid cached/reasoning usage")
	}
	return values, nil
}
func estimateCost(run, ratesPath, summaryPath, archiveHash string) (M, map[string][]M, error) {
	rates, e := readKeyed(ratesPath, "id")
	if e != nil {
		return nil, nil, e
	}
	answers, e := readKeyed(filepath.Join(run, "answers.jsonl"), "id")
	if e != nil {
		return nil, nil, e
	}
	summary, e := oneRow(summaryPath)
	if e != nil {
		return nil, nil, e
	}
	archivedSummary, e := oneRow(filepath.Join(run, "summary.jsonl"))
	if e != nil {
		return nil, nil, e
	}
	if !same(summary, archivedSummary) {
		return nil, nil, fmt.Errorf("summary does not match frozen archive")
	}
	searches := map[string]map[string]bool{}
	counts := map[string]int{}
	rows := map[string]M{}
	for _, arm := range costArms {
		searches[arm] = map[string]bool{}
		for _, phase := range costPhases {
			rows[arm+"/"+phase] = M{"id": arm + "/" + phase, "arm": arm, "phase": phase, "model": costModels[phase], "requests": float64(0), "input_tokens": float64(0), "cached_input_tokens": float64(0), "output_tokens": float64(0), "reasoning_tokens": float64(0)}
		}
	}
	for _, a := range answers {
		arm := str(a["arm"])
		if searches[arm] == nil {
			return nil, nil, fmt.Errorf("unknown answer arm")
		}
		counts[arm]++
		for _, sid := range arr(a["search_ids"]) {
			searches[arm][str(sid)] = true
		}
	}
	if counts["baseline"] == 0 || counts["baseline"] != counts["jev_filtered"] {
		return nil, nil, fmt.Errorf("unpaired answers")
	}
	nq := int64(counts["baseline"])
	receipts := []M{}
	requestIDs := map[string]bool{}
	e = scan(filepath.Join(run, "requests.jsonl"), func(b []byte) error {
		var r M
		if e := json.Unmarshal(b, &r); e != nil {
			return e
		}
		id := str(r["id"])
		if id == "" || requestIDs[id] {
			return fmt.Errorf("missing/duplicate request ID")
		}
		requestIDs[id] = true
		phase, key := str(r["phase"]), str(r["key"])
		model, ok := costModels[phase]
		if !ok || strings.HasPrefix(key, "probe/") {
			return nil
		}
		if num(r["status"]) != 200 || str(r["error"]) != "" {
			return fmt.Errorf("unpriced failed answering attempt %s", id)
		}
		p := obj(r["payload"])
		parts := strings.Split(str(p["model"]), "/")
		if parts[len(parts)-1] != model {
			return fmt.Errorf("unexpected model for %s", id)
		}
		tier := str(p["service_tier"])
		if tier != "" && tier != "default" && tier != "auto" && tier != "standard" {
			return fmt.Errorf("unsupported service tier %s", tier)
		}
		usage, e := tokenUsage(r)
		if e != nil {
			return e
		}
		for _, arm := range costArms {
			belongs := (phase == "answer" && strings.Contains(key, "/"+arm+"/")) || (phase == "filter" && arm == "jev_filtered")
			if phase == "embedding" || phase == "rerank" {
				parts := strings.Split(key, "/")
				if len(parts) < 2 {
					return fmt.Errorf("invalid search request key")
				}
				belongs = searches[arm][parts[1]]
			}
			if !belongs {
				continue
			}
			row := rows[arm+"/"+phase]
			row["requests"] = num(row["requests"]) + 1
			receipt := M{"id": arm + "/" + id, "arm": arm, "request_id": id, "phase": phase, "model": model}
			for field, v := range usage {
				row[field] = num(row[field]) + num(v)
				receipt[field] = v
			}
			receipts = append(receipts, receipt)
		}
		return nil
	})
	if e != nil {
		return nil, nil, e
	}
	for _, arm := range costArms {
		resources := obj(obj(summary["resources"])[arm])
		for phase, name := range map[string]string{"answer": "qa", "filter": "filter"} {
			for _, field := range []string{"input_tokens", "output_tokens", "requests"} {
				if rows[arm+"/"+phase][field] != obj(resources[name])[field] {
					return nil, nil, fmt.Errorf("frozen resource mismatch %s/%s/%s", arm, phase, field)
				}
			}
		}
		if num(rows[arm+"/embedding"]["input_tokens"])+num(rows[arm+"/rerank"]["input_tokens"]) != num(obj(resources["search"])["input_tokens"]) {
			return nil, nil, fmt.Errorf("search resource mismatch %s", arm)
		}
	}
	totals := map[string]map[string]*big.Rat{}
	for _, scenario := range costScenarios {
		totals[scenario] = map[string]*big.Rat{"baseline": new(big.Rat), "jev_filtered": new(big.Rat)}
	}
	components := []M{}
	for _, arm := range costArms {
		for _, phase := range costPhases {
			row := rows[arm+"/"+phase]
			rate := rates[str(row["model"])]
			rs := []*big.Rat{}
			for _, field := range []string{"input", "cached_input", "output"} {
				r, ok := new(big.Rat).SetString(str(rate[field+"_usd_per_million"]))
				if !ok || r.Sign() < 0 {
					return nil, nil, fmt.Errorf("invalid rate %s/%s", row["model"], field)
				}
				rs = append(rs, r)
			}
			ri, rc, ro := rs[0], rs[1], rs[2]
			input, cached, output := int64(num(row["input_tokens"])), int64(num(row["cached_input_tokens"])), int64(num(row["output_tokens"]))
			for _, scenario := range costScenarios {
				cacheRate := rc
				cacheTokens := cached
				if scenario == "no_cache" {
					cacheTokens = 0
				}
				if scenario == "qwen_default_cache" && strings.HasPrefix(str(row["model"]), "qwen") {
					cacheRate = new(big.Rat).Quo(ri, big.NewRat(2, 1))
				}
				cost := priceTokens(input, cacheTokens, output, ri, cacheRate, ro)
				row[scenario+"_usd"] = decimal(cost)
				totals[scenario][arm].Add(totals[scenario][arm], cost)
			}
			components = append(components, row)
		}
	}
	comparisons := []M{}
	for _, scenario := range costScenarios {
		b, f := totals[scenario]["baseline"], totals[scenario]["jev_filtered"]
		if b.Sign() == 0 {
			return nil, nil, fmt.Errorf("baseline cost is zero")
		}
		saving := new(big.Rat).Sub(b, f)
		percent := new(big.Rat).Quo(new(big.Rat).Mul(saving, big.NewRat(100, 1)), b)
		comparisons = append(comparisons, M{"id": scenario, "baseline_usd": decimal(b), "jev_filtered_usd": decimal(f), "savings_usd": decimal(saving), "savings_percent": percent.FloatString(26), "baseline_usd_per_question": decimal(new(big.Rat).Quo(b, big.NewRat(nq, 1))), "jev_filtered_usd_per_question": decimal(new(big.Rat).Quo(f, big.NewRat(nq, 1)))})
	}
	summaryHash, e := fileHash(summaryPath)
	if e != nil {
		return nil, nil, e
	}
	ratesHash, e := fileHash(ratesPath)
	if e != nil {
		return nil, nil, e
	}
	manifest := M{"id": "cost-analysis", "source_archive_sha256": archiveHash, "source_summary_sha256": summaryHash, "rates_sha256": ratesHash, "implementation": "jev-search-result-narrower estimate-cost (Go/Cobra)", "questions_per_arm": nq, "scope": "Answering model API costs; shared searches allocated once to each arm that used them; includes follow-up search.", "excluded": []string{"Turbopuffer queries/storage/indexing", "historical corpus embedding", "evaluation judges", "preflight", "compute host", "credits/discount contracts/taxes"}, "reasoning_accounting": "Already included in completion_tokens; never added twice.", "cache_sensitivity_source": "https://docs.fireworks.ai/guides/prompt-caching", "reconciled_to_frozen_summary": true}
	if info, ok := debug.ReadBuildInfo(); ok {
		manifest["go_version"] = info.GoVersion
		for _, setting := range info.Settings {
			if strings.HasPrefix(setting.Key, "vcs") {
				manifest[setting.Key] = setting.Value
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	executableHash, err := fileHash(executable)
	if err != nil {
		return nil, nil, err
	}
	manifest["executable_sha256"] = executableHash
	return M{"components": components, "comparisons": comparisons}, map[string][]M{"model_costs": components, "summary": comparisons, "request_usage": receipts, "manifest": {manifest}}, nil
}
