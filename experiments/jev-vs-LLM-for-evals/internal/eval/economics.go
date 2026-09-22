package eval

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PriceReceipt never treats absent usage/prices as a zero-cost request.
func PriceReceipt(r M, pricing M) M {
	reply := obj(r["response"])
	u := obj(reply["usage"])
	model := str(r["model"])
	rate := obj(obj(pricing["models"])[model])
	x := M{"key": r["key"], "model": model, "resolved_model": reply["model"], "latency_s": r["latency_s"], "transport_ok": r["transport_ok"], "attempt": r["attempt"], "terminal": r["terminal"], "input_tokens": nil, "cached_input_tokens": nil, "output_tokens": nil, "reasoning_tokens": nil, "estimated_input_usd": nil, "estimated_output_usd": nil, "estimated_total_usd": nil, "price_source": rate["source"], "truncated": false}
	input, hasInput := u["input_tokens"]
	if !hasInput {
		input, hasInput = u["prompt_tokens"]
	}
	output, hasOutput := u["output_tokens"]
	if !hasOutput {
		output, hasOutput = u["completion_tokens"]
	}
	hasInput = hasInput && validTokenCount(input)
	hasOutput = hasOutput && validTokenCount(output)
	if hasInput {
		x["input_tokens"] = input
	}
	if hasOutput {
		x["output_tokens"] = output
	}
	details := obj(u["prompt_tokens_details"])
	if len(details) == 0 {
		details = obj(u["input_tokens_details"])
	}
	cached, hasCache := details["cached_tokens"]
	hasCache = hasCache && validTokenCount(cached)
	if hasCache {
		x["cached_input_tokens"] = cached
	}
	rd := obj(u["completion_tokens_details"])
	if len(rd) == 0 {
		rd = obj(u["output_tokens_details"])
	}
	if validTokenCount(rd["reasoning_tokens"]) {
		x["reasoning_tokens"] = rd["reasoning_tokens"]
	}
	for _, v := range arr(reply["choices"]) {
		if obj(v)["finish_reason"] == "length" {
			x["truncated"] = true
		}
	}
	if hasInput && (num(input) < 0 || (hasCache && (num(cached) < 0 || num(cached) > num(input)))) {
		x["unpriced_reason"] = "invalid input/cache usage"
		return x
	}
	if hasOutput && num(output) < 0 {
		panic("invalid output usage")
	}
	// A cached rate is required if cached tokens are reported. Unknown cache counts
	// use the uncached rate, explicitly identified as an assumption rather than fact.
	x["cache_usage_reported"] = hasCache
	tier := str(obj(r["payload"])["service_tier"])
	if tier != "" && tier != "standard" {
		x["unpriced_reason"] = "nonstandard service tier"
		return x
	}
	if hasInput && rate["input_per_million"] != nil {
		in, cache := num(input), num(cached)
		if !hasCache {
			cache = 0
			x["cache_assumption"] = "no cache discount assumed; provider did not report cached tokens"
		}
		if cache == 0 || rate["cached_input_per_million"] != nil {
			x["estimated_input_usd"] = ((in-cache)*num(rate["input_per_million"]) + cache*num(rate["cached_input_per_million"])) / 1e6
		}
	}
	if hasOutput && rate["output_per_million"] != nil {
		x["estimated_output_usd"] = num(output) * num(rate["output_per_million"]) / 1e6
	}
	if x["estimated_input_usd"] != nil && x["estimated_output_usd"] != nil {
		x["estimated_total_usd"] = num(x["estimated_input_usd"]) + num(x["estimated_output_usd"])
	}
	return x
}
func percentile(xs []float64, q float64) any {
	if len(xs) == 0 {
		return nil
	}
	xs = append([]float64{}, xs...)
	sort.Float64s(xs)
	position := float64(len(xs)-1) * q
	lo, hi := int(math.Floor(position)), int(math.Ceil(position))
	return xs[lo] + (xs[hi]-xs[lo])*(position-float64(lo))
}
func divide(a any, n int) any {
	if a == nil || n == 0 {
		return nil
	}
	return num(a) / float64(n)
}
func aggregate(requests []M) M {
	a := M{"api_attempts": len(requests), "transport_failures": 0, "truncated_replies": 0, "missing_usage_attempts": 0, "unpriced_attempts": 0, "input_tokens": 0.0, "output_tokens": 0.0, "reported_cached_input_tokens": 0.0, "reported_reasoning_tokens": 0.0, "known_estimated_input_usd": 0.0, "known_estimated_output_usd": 0.0, "known_estimated_total_usd": 0.0, "estimated_total_usd": nil, "summed_request_latency_s": 0.0, "retry_backoff_s": 0.0}
	latencies, successes := []float64{}, []float64{}
	missing, unpriced, unknownIn, unknownOut := 0, 0, 0, 0
	for _, r := range requests {
		latencies = append(latencies, num(r["latency_s"]))
		a["summed_request_latency_s"] = num(a["summed_request_latency_s"]) + num(r["latency_s"])
		a["retry_backoff_s"] = num(a["retry_backoff_s"]) + num(r["retry_backoff_s"])
		if !yes(r["transport_ok"]) {
			a["transport_failures"] = num(a["transport_failures"]) + 1
		} else {
			successes = append(successes, num(r["latency_s"]))
		}
		if yes(r["truncated"]) {
			a["truncated_replies"] = num(a["truncated_replies"]) + 1
		}
		if r["input_tokens"] == nil || r["output_tokens"] == nil {
			missing++
		}
		if r["estimated_total_usd"] == nil {
			unpriced++
		}
		if r["estimated_input_usd"] == nil {
			unknownIn++
		}
		if r["estimated_output_usd"] == nil {
			unknownOut++
		}
		for dest, source := range map[string]string{"input_tokens": "input_tokens", "output_tokens": "output_tokens", "reported_cached_input_tokens": "cached_input_tokens", "reported_reasoning_tokens": "reasoning_tokens", "known_estimated_input_usd": "estimated_input_usd", "known_estimated_output_usd": "estimated_output_usd", "known_estimated_total_usd": "estimated_total_usd"} {
			a[dest] = num(a[dest]) + num(r[source])
		}
	}
	a["missing_usage_attempts"], a["unpriced_attempts"] = missing, unpriced
	a["usage_complete"] = missing == 0
	a["estimated_input_usd"], a["estimated_output_usd"] = nil, nil
	if unknownIn == 0 {
		a["estimated_input_usd"] = a["known_estimated_input_usd"]
	}
	if unknownOut == 0 {
		a["estimated_output_usd"] = a["known_estimated_output_usd"]
	}
	if unpriced == 0 {
		a["estimated_total_usd"] = a["known_estimated_total_usd"]
	}
	a["request_latency_p50_s"], a["request_latency_p95_s"], a["successful_request_latency_p50_s"] = percentile(latencies, .5), percentile(latencies, .95), percentile(successes, .5)
	a["output_tokens_per_request_second"] = nil
	if num(a["summed_request_latency_s"]) > 0 && missing == 0 {
		a["output_tokens_per_request_second"] = num(a["output_tokens"]) / num(a["summed_request_latency_s"])
	}
	return a
}
func Economics(out string, pricing M) (report M, err error) {
	defer Recover(&err)
	jobs := arr(ReadJSON(filepath.Join(out, "jobs.json")))
	results := Lines(filepath.Join(out, "results.jsonl"))
	raw := Lines(filepath.Join(out, "requests.jsonl"))
	byJob := map[string]M{}
	byResult := map[string]M{}
	for _, v := range jobs {
		j := obj(v)
		byJob[str(j["key"])] = j
	}
	for _, r := range results {
		byResult[str(r["key"])] = r
	}
	priced := []M{}
	jobRequests := map[string][]M{}
	backendRequests := map[string][]M{}
	spans := map[string][2]time.Time{}
	var runStart, runEnd time.Time
	for _, r := range raw {
		key, _, _ := strings.Cut(str(r["key"]), "/")
		j, ok := byJob[key]
		if !ok {
			panic("orphan request receipt")
		}
		x := PriceReceipt(r, pricing)
		x["retry_backoff_s"] = r["retry_backoff_s"]
		x["owner_job"] = key
		x["judge_model"] = j["model"]
		x["benchmark"] = j["benchmark"]
		x["method"] = j["method"]
		x["helper"] = r["model"] != j["model"]
		priced = append(priced, x)
		jobRequests[key] = append(jobRequests[key], x)
		backendRequests[str(r["model"])] = append(backendRequests[str(r["model"])], x)
		start, e := time.Parse(time.RFC3339Nano, str(r["started_utc"]))
		check(e)
		end := start.Add(time.Duration(num(r["latency_s"]) * float64(time.Second)))
		span := spans[key]
		if span[0].IsZero() || start.Before(span[0]) {
			span[0] = start
		}
		if end.After(span[1]) {
			span[1] = end
		}
		spans[key] = span
		if runStart.IsZero() || start.Before(runStart) {
			runStart = start
		}
		if end.After(runEnd) {
			runEnd = end
		}
	}
	nativeTimes := map[string]float64{}
	for _, r := range Lines(filepath.Join(out, "job-timings.jsonl")) {
		nativeTimes[str(r["key"])] += num(r["active_wall_s"])
	}
	perJob := []M{}
	groups := map[string][]M{}
	groupRequests := map[string][]M{}
	for _, v := range jobs {
		j := obj(v)
		key := str(j["key"])
		rs := jobRequests[key]
		a := aggregate(rs)
		a["key"], a["benchmark"], a["method"], a["model"], a["subset"], a["case_id"] = key, j["benchmark"], j["method"], j["model"], obj(j["row"])["subset"], obj(j["row"])["id"]
		r, complete := byResult[key]
		a["complete"] = complete
		a["valid"] = r["valid"]
		a["published_score"] = r["published_score"]
		a["observed_stage_span_s"] = nil
		if span, ok := spans[key]; ok {
			a["observed_stage_span_s"] = span[1].Sub(span[0]).Seconds()
		}
		a["native_active_wall_s"] = nil
		if t, ok := nativeTimes[key]; ok {
			a["native_active_wall_s"] = t
		}
		helpers := []M{}
		for _, r := range rs {
			if yes(r["helper"]) {
				helpers = append(helpers, r)
			}
		}
		a["helpers"] = aggregate(helpers)
		perJob = append(perJob, a)
		g := str(j["benchmark"]) + "\t" + str(j["method"]) + "\t" + str(j["model"])
		groups[g] = append(groups[g], a)
		groupRequests[g] = append(groupRequests[g], rs...)
	}
	summaries := []M{}
	names := []string{}
	for k := range groups {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		js := groups[k]
		a := aggregate(groupRequests[k])
		a["benchmark"], a["method"], a["model"] = js[0]["benchmark"], js[0]["method"], js[0]["model"]
		completed, valid := 0, 0
		spans, wall := []float64{}, []float64{}
		for _, j := range js {
			if yes(j["complete"]) {
				completed++
				if j["observed_stage_span_s"] != nil {
					spans = append(spans, num(j["observed_stage_span_s"]))
				}
				if j["native_active_wall_s"] != nil {
					wall = append(wall, num(j["native_active_wall_s"]))
				}
			}
			if yes(j["valid"]) {
				valid++
			}
		}
		a["planned_jobs"], a["completed_jobs"], a["valid_jobs"], a["complete"] = len(js), completed, valid, completed == len(js)
		a["estimated_usd_per_completed_job"], a["estimated_usd_per_valid_job"] = nil, nil
		if completed == len(js) {
			a["estimated_usd_per_completed_job"] = divide(a["estimated_total_usd"], completed)
			a["estimated_usd_per_valid_job"] = divide(a["estimated_total_usd"], valid)
		}
		a["job_stage_span_p50_s"], a["job_stage_span_p95_s"] = percentile(spans, .5), percentile(spans, .95)
		a["native_job_wall_p50_s"], a["native_job_wall_p95_s"] = percentile(wall, .5), percentile(wall, .95)
		a["input_tokens_per_planned_job"], a["output_tokens_per_planned_job"] = divide(a["input_tokens"], len(js)), divide(a["output_tokens"], len(js))
		summaries = append(summaries, a)
	}
	backends := M{}
	for model, rs := range backendRequests {
		backends[model] = aggregate(rs)
	}
	report = M{"schema_version": 1, "pricing": pricing, "complete": len(results) == len(jobs), "planned_jobs": len(jobs), "completed_jobs": len(results), "request_observation_wall_s": runEnd.Sub(runStart).Seconds(), "totals": aggregate(priced), "backends": backends, "comparisons": summaries, "jobs": perJob, "requests": priced, "notes": []string{"Dollar values are estimates from saved provider usage and dated list prices, not invoices. Missing usage/pricing leaves total cost null; known sums remain explicitly partial.", "Reported input tokens include cached tokens; reported reasoning tokens are a subset of output tokens. Do not add either twice.", "Owner-model comparisons include all helper requests and retries. Backend totals answer a different question.", "Latency covers full HTTP requests, not time to first token. No streaming TTFT was measured. Output tokens per request second is not decode throughput.", "Historical job stage spans include gaps between requests and restarts. Native active wall sums recorded execution segments and excludes downtime between invocations.", "Cached prompt tokens use the published cache rate; missing cache counts assume no discount. Legacy retry backoff was not separately recorded.", "Comparisons share cases within each method; do not pool across unequal methods or infer significance from the tiny validation sample."}}
	WriteJSON(filepath.Join(out, "economics.json"), report)
	WriteJSON(filepath.Join(out, "pricing.snapshot.json"), pricing)
	return
}
func EconomicMarkdown(report M) string {
	var b strings.Builder
	b.WriteString("# Cost, tokens, and speed\n\nDollar figures are **list-price estimates**, not reconciled invoices. Unknown costs are shown as `unknown`; partial known sums are in economics.json. Each method/model includes helper calls and retries.\n\n")
	b.WriteString("| Benchmark / method | Model | Jobs complete / planned | Input tokens | Output tokens | Input USD | Output USD | Total USD | Request p50 / p95 (s) | Job span p50 (s) |\n|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	f := func(v any) string {
		if v == nil {
			return "unknown"
		}
		return fmt.Sprintf("%.6f", num(v))
	}
	for _, v := range report["comparisons"].([]M) {
		fmt.Fprintf(&b, "| %s / %s | %s | %v / %v | %.0f | %.0f | %s | %s | %s | %s / %s | %s |\n", v["benchmark"], v["method"], filepath.Base(str(v["model"])), v["completed_jobs"], v["planned_jobs"], num(v["input_tokens"]), num(v["output_tokens"]), f(v["estimated_input_usd"]), f(v["estimated_output_usd"]), f(v["estimated_total_usd"]), f(v["request_latency_p50_s"]), f(v["request_latency_p95_s"]), f(v["job_stage_span_p50_s"]))
	}
	b.WriteString("\n## Measurement notes\n\n")
	for _, n := range report["notes"].([]string) {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	b.WriteString("\n[TypeSafe pricing](https://typesafe.ai/blog/introducing-system-one-models-and-jev) · [Fireworks Standard pricing](https://docs.fireworks.ai/serverless/pricing), checked September 17, 2026. Full per-request, per-case, backend, and method aggregates: [economics.json](economics.json).\n")
	return b.String()
}

func validTokenCount(v any) bool {
	switch v.(type) {
	case float64, int, int64:
	default:
		return false
	}
	n := num(v)
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n == math.Trunc(n)
}
