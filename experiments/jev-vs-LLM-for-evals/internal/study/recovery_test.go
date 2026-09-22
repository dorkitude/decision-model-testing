package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"path/filepath"
	"testing"
)

func TestLoadSingleRunPreservesReceiptAccounting(t *testing.T) {
	t.Skip("integration requires upstream data or private historical fixtures; excluded from source-free offline suite")
	rows, manifest, err := LoadRun(filepath.Join("..", "..", "results", "robust-preflight-v1", "shards", "0000"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != int(n(manifest["jobs"])) || len(rows) == 0 {
		t.Fatal("single-run extraction incomplete")
	}
}
func TestRecoveryUsesAllRetriesAndPaysBoth(t *testing.T) {
	p := M{"key": "x", "benchmark": "llmbar", "method": "Vanilla", "model": "jev-latest", "case_id": "x", "subset": "Natural", "comparison_content_sha256": "same", "valid": false, "published_score": 1., "accounting_usd": 1., "input_tokens": 10.}
	q := M{}
	for k, v := range p {
		q[k] = v
	}
	q["valid"] = true
	q["published_score"] = 0.
	q["accounting_usd"] = 2.
	primary, retry := []M{p}, []M{q}
	source := M{"jobs": 1}
	jobs := []any{M{"key": "x"}}
	plan := M{"protocol": "service-error-sensitivity-v1", "jobs": jobs, "source_manifest_sha256": eval.Hash(eval.Canon(source)), "source_records_sha256": eval.Hash(eval.Canon(primary))}
	manifest := M{"jobs": 1, "run_manifest": M{"jobs_sha256": eval.Hash(eval.Canon(jobs)), "pricing_sha256": eval.Hash(eval.Canon(nil))}}
	got := MergeServiceRecovery(primary, retry, plan, source, manifest)
	if n(got[0]["published_score"]) != 0 || !b(got[0]["valid"]) || n(got[0]["accounting_usd"]) != 3 || n(got[0]["input_tokens"]) != 20 {
		t.Fatal(got)
	}
	if n(p["accounting_usd"]) != 1 || n(p["published_score"]) != 1 {
		t.Fatal("original mutated")
	}
	q["comparison_content_sha256"] = "changed"
	defer func() {
		if recover() == nil {
			t.Error("changed retry input accepted")
		}
	}()
	MergeServiceRecovery(primary, retry, plan, source, manifest)
}
