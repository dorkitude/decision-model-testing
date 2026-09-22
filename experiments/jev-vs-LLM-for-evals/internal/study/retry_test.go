package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"path/filepath"
	"testing"
)

func TestServiceRetrySelectionAndFrozenJobs(t *testing.T) {
	dir := t.TempDir()
	jobs := []M{{"key": "bad"}, {"key": "truncated"}, {"key": "recovered"}}
	eval.WriteJSON(filepath.Join(dir, "shards/0000/jobs.json"), jobs)
	rows := []M{{"key": "bad", "valid": false, "terminal_service_failures": 1, "accounting_usd": .2}, {"key": "truncated", "valid": false, "terminal_service_failures": 0}, {"key": "recovered", "valid": true, "terminal_service_failures": 1}}
	manifest := M{"config": M{"concurrency": 32, "max_tokens_floor": 8192}, "shards": []any{M{"directory": "shards/0000", "jobs_sha256": eval.Hash(eval.Canon(jobs))}}}
	got := ServiceRetryPlan(dir, rows, manifest, true)
	if len(got["jobs"].([]M)) != 1 || got["jobs"].([]M)[0]["key"] != "bad" {
		t.Fatal(got)
	}
	if n(m(got["config"])["concurrency"]) != 4 || n(m(manifest["config"])["concurrency"]) != 32 || n(m(got["config"])["max_tokens_floor"]) != 8192 {
		t.Fatal("configuration mutated or inference budget changed")
	}
	if RetryableServiceError("HTTP 401") || RetryableServiceError("invalid_response") || !RetryableServiceError("HTTP 429") {
		t.Fatal("retry policy changed")
	}
	defer func() {
		if recover() == nil {
			t.Error("incomplete source accepted")
		}
	}()
	ServiceRetryPlan(dir, rows, manifest, false)
}
