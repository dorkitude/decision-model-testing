package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"path/filepath"
)

func LoadRun(dir string) (rows []M, manifest M, err error) {
	defer eval.Recover(&err)
	original := m(eval.ReadJSON(filepath.Join(dir, "manifest.json")))
	pricing := m(eval.ReadJSON(filepath.Join(dir, "pricing.snapshot.json")))
	if original["pricing_sha256"] != eval.Hash(eval.Canon(pricing)) {
		panic("retry run pricing hash mismatch")
	}
	synthetic := M{"jobs": original["jobs"], "config": original["config"], "pricing": pricing, "shards": []any{M{"directory": ".", "jobs": original["jobs"], "jobs_sha256": original["jobs_sha256"]}}, "run_manifest": original}
	return loadManifest(dir, synthetic)
}
func MergeServiceRecovery(primary, retry []M, plan M, sourceManifest, retryManifest M) []M {
	if plan["protocol"] != "service-error-sensitivity-v1" || plan["source_manifest_sha256"] != eval.Hash(eval.Canon(sourceManifest)) || plan["source_records_sha256"] != eval.Hash(eval.Canon(primary)) {
		panic("service recovery source provenance mismatch")
	}
	if len(primary) != int(n(sourceManifest["jobs"])) || len(retry) != int(n(retryManifest["jobs"])) {
		panic("service recovery requires both runs complete")
	}
	if m(retryManifest["run_manifest"])["jobs_sha256"] != eval.Hash(eval.Canon(plan["jobs"])) {
		panic("retry run differs from frozen selected jobs")
	}
	if eval.Hash(eval.Canon(m(retryManifest["run_manifest"])["config"])) != eval.Hash(eval.Canon(plan["config"])) || m(retryManifest["run_manifest"])["pricing_sha256"] != eval.Hash(eval.Canon(plan["pricing"])) {
		panic("retry inference configuration or pricing differs from plan")
	}
	selected := map[string]bool{}
	for _, v := range a(plan["jobs"]) {
		selected[s(m(v)["key"])] = true
	}
	byKey := map[string]M{}
	for _, r := range retry {
		key := s(r["key"])
		if !selected[key] || byKey[key] != nil {
			panic("unexpected or duplicate retry result")
		}
		byKey[key] = r
	}
	if len(byKey) != len(selected) {
		panic("incomplete selected retry results")
	}
	out := []M{}
	for _, p := range primary {
		key := s(p["key"])
		r := M{}
		for k, v := range p {
			r[k] = v
		}
		if selected[key] {
			q := byKey[key]
			if q == nil || q["comparison_content_sha256"] != p["comparison_content_sha256"] {
				panic("retry content mismatch")
			}
			for _, field := range []string{"benchmark", "method", "model", "case_id", "subset"} {
				if q[field] != p[field] {
					panic("retry identity mismatch")
				}
			}
			for k, v := range q {
				r[k] = v
			}
			r["original_source_shard"] = p["source_shard"]
			r["service_retry_applied"] = true
			for _, field := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens", "known_input_usd", "known_output_usd", "known_total_usd", "accounting_usd", "unknown_cost_attempts", "request_attempts", "orphan_reservations", "transport_failures", "truncated_replies", "helper_attempts", "helper_accounting_usd", "native_wall_seconds", "request_seconds", "terminal_service_failures"} {
				r[field] = n(p[field]) + n(q[field])
			}
			for _, field := range []string{"error_attempts", "resolved_models"} {
				merged := M{}
				for k, v := range m(p[field]) {
					merged[k] = v
				}
				for k, v := range m(q[field]) {
					if field == "error_attempts" {
						plus(merged, k, n(v))
					} else {
						merged[k] = v
					}
				}
				r[field] = merged
			}
			r["request_latencies"] = append(append([]any{}, a(p["request_latencies"])...), a(q["request_latencies"])...)
			r["estimated_total_usd"] = nil
			if n(r["unknown_cost_attempts"]) == 0 {
				r["estimated_total_usd"] = r["known_total_usd"]
			}
		}
		out = append(out, r)
	}
	return out
}

func RecoveryReport(primary, recovered []M, replicates int) M {
	group := func(records []M) map[string][]Unit {
		out := map[string][]Unit{}
		for _, u := range Units(records) {
			key := u.Benchmark + "\t" + u.Method + "\t" + u.Model
			out[key] = append(out[key], u)
		}
		return out
	}
	before, after := group(primary), group(recovered)
	pairedReports := []M{}
	for _, key := range orderedKeys(before) {
		r := PairComparison(after[key], before[key], replicates)
		r["scope"] = "recovered-policy minus original; same model with one predefined supplementary service-error pass"
		pairedReports = append(pairedReports, r)
	}
	selected, validAfter := 0, 0
	for _, r := range recovered {
		if b(r["service_retry_applied"]) {
			selected++
			if b(r["valid"]) {
				validAfter++
			}
		}
	}
	return M{"scope": "all selected rerun outcomes used, including failures; both original and rerun costs charged; changes reflect service recovery, not improved model capability", "selected_jobs": selected, "selected_jobs_valid_after_retry": validAfter, "scores": eval.Scores(recovered), "resources_and_strict_scores": StrictSummaries(recovered), "paired_recovered_minus_original": pairedReports}
}
