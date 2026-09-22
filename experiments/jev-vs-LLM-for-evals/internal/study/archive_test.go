package study

import (
	"compress/gzip"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveAuditDetectsChangedCompressedEvidence(t *testing.T) {
	dir := t.TempDir()
	shard := filepath.Join(dir, "shards/0000")
	jobs := []any{M{"key": "fixture"}}
	eval.WriteJSON(filepath.Join(shard, "jobs.json"), jobs)
	eval.WriteJSON(filepath.Join(dir, "campaign.json"), M{"jobs": 1, "shards": []any{M{"directory": "shards/0000", "jobs": 1, "jobs_sha256": eval.Hash(eval.Canon(jobs))}}})
	eval.WriteJSON(filepath.Join(shard, "status-go.json"), M{"complete": true})
	eval.WriteJSON(filepath.Join(shard, "go-replay-verification.json"), M{"complete": true, "replayed_jobs": 1})
	raw := []byte("recorded evidence\n")
	write := func(data []byte) {
		f, e := os.Create(filepath.Join(shard, "requests.jsonl.gz"))
		if e != nil {
			t.Fatal(e)
		}
		w := gzip.NewWriter(f)
		w.Write(data)
		w.Close()
		f.Close()
	}
	write(raw)
	eval.WriteJSON(filepath.Join(shard, "archive.json"), M{"requests.jsonl": M{"file": "requests.jsonl.gz", "uncompressed_sha256": eval.Hash(raw), "uncompressed_bytes": len(raw)}})
	got, err := AuditCompletedArchives(dir)
	if err != nil || !b(got["complete"]) {
		t.Fatal(got, err)
	}
	write([]byte("changed evidence\n"))
	if _, err = AuditCompletedArchives(dir); err == nil {
		t.Fatal("changed archive accepted")
	}
}
