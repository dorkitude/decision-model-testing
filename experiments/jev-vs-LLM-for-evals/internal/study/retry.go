package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"path/filepath"
	"sort"
)

func RetryableServiceError(category string) bool {
	switch category {
	case "timeout", "transport_error", "HTTP 429", "HTTP 500", "HTTP 502", "HTTP 503", "HTTP 504", "HTTP 529":
		return true
	}
	return false
}
func RetryEligible(r M) bool { return !b(r["valid"]) && n(r["terminal_service_failures"]) > 0 }

// ServiceRetryPlan freezes a single supplementary whole-job pass for failures
// attributable to retryable terminal service errors. No gold label is consulted.
func ServiceRetryPlan(dir string, records []M, manifest M, complete bool) M {
	if !complete {
		panic("service retry selection requires a complete campaign")
	}
	selected := map[string]M{}
	for _, r := range records {
		if RetryEligible(r) {
			selected[s(r["key"])] = r
		}
	}
	jobs := []M{}
	origins := []M{}
	for _, v := range a(manifest["shards"]) {
		spec := m(v)
		raw := a(eval.ReadJSON(filepath.Join(dir, s(spec["directory"]), "jobs.json")))
		if eval.Hash(eval.Canon(raw)) != spec["jobs_sha256"] {
			panic("retry source jobs hash mismatch")
		}
		for _, v := range raw {
			job := m(v)
			if r, ok := selected[s(job["key"])]; ok {
				jobs = append(jobs, job)
				origins = append(origins, M{"key": job["key"], "source_shard": spec["directory"], "terminal_service_failures": r["terminal_service_failures"], "original_accounting_usd": r["accounting_usd"]})
				delete(selected, s(job["key"]))
			}
		}
	}
	if len(selected) > 0 {
		panic("selected retry job missing from source campaign")
	}
	sort.Slice(jobs, func(i, j int) bool { return s(jobs[i]["key"]) < s(jobs[j]["key"]) })
	config := M{}
	for k, v := range m(manifest["config"]) {
		config[k] = v
	}
	config["concurrency"] = 4
	config["max_requests"] = 20000
	config["max_estimated_usd"] = 100.
	return M{"protocol": "service-error-sensitivity-v1", "scope": "one supplementary whole-job pass; original labels, scores and costs retained; service recovery is not evidence of better judging", "source_campaign": dir, "source_manifest_sha256": eval.Hash(eval.Canon(manifest)), "source_records_sha256": eval.Hash(eval.Canon(records)), "selection": "invalid jobs with terminal timeout, transport error, HTTP 429/500/502/503/504/529; excludes pure truncation/parser failures and completed retries", "jobs": jobs, "config": config, "pricing": manifest["pricing"], "origins": origins}
}
