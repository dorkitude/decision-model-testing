package study

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"io"
	"os"
	"path/filepath"
)

// AuditCompletedArchives checks sealed evidence without touching active files.
// Replay proof is checked as recorded; this does not rerun inference or replay.
func AuditCompletedArchives(dir string) (report M, err error) {
	defer eval.Recover(&err)
	manifest := m(eval.ReadJSON(filepath.Join(dir, "campaign.json")))
	shards := []M{}
	jobs := 0
	for _, v := range a(manifest["shards"]) {
		spec := m(v)
		path := filepath.Join(dir, s(spec["directory"]))
		archivePath := filepath.Join(path, "archive.json")
		if _, e := os.Stat(archivePath); os.IsNotExist(e) {
			continue
		}
		status := m(eval.ReadJSON(filepath.Join(path, "status-go.json")))
		proof := m(eval.ReadJSON(filepath.Join(path, "go-replay-verification.json")))
		if !b(status["complete"]) || !b(proof["complete"]) || n(proof["replayed_jobs"]) != n(spec["jobs"]) {
			panic("sealed shard lacks complete recorded replay proof")
		}
		frozenJobs := a(eval.ReadJSON(filepath.Join(path, "jobs.json")))
		if len(frozenJobs) != int(n(spec["jobs"])) || eval.Hash(eval.Canon(frozenJobs)) != spec["jobs_sha256"] {
			panic("sealed shard job identity mismatch")
		}
		archive := m(eval.ReadJSON(archivePath))
		checked := M{}
		for _, name := range orderedKeys(archive) {
			entry := m(archive[name])
			filename := s(entry["file"])
			if filename == "" || filepath.Base(filename) != filename {
				panic("unsafe archive filename")
			}
			f, e := os.Open(filepath.Join(path, filename))
			must(e)
			compressedHash := sha256.New()
			reader, e := gzip.NewReader(io.TeeReader(f, compressedHash))
			if e != nil {
				f.Close()
				panic(e)
			}
			uncompressedHash := sha256.New()
			size, copyErr := io.Copy(uncompressedHash, reader)
			closeErr := reader.Close()
			fileErr := f.Close()
			must(copyErr)
			must(closeErr)
			must(fileErr)
			digest := hex.EncodeToString(uncompressedHash.Sum(nil))
			if digest != entry["uncompressed_sha256"] || float64(size) != n(entry["uncompressed_bytes"]) {
				panic(fmt.Sprintf("archive checksum/length mismatch: %s/%s", spec["directory"], filename))
			}
			checked[filename] = M{"compressed_sha256": hex.EncodeToString(compressedHash.Sum(nil)), "uncompressed_sha256": digest, "uncompressed_bytes": size}
		}
		shards = append(shards, M{"directory": spec["directory"], "jobs": spec["jobs"], "jobs_sha256": spec["jobs_sha256"], "archive_manifest_sha256": eval.Hash(eval.Canon(archive)), "recorded_replay_complete": true, "verified_compressed_files": checked})
		jobs += int(n(spec["jobs"]))
	}
	return M{"scope": "sealed compressed evidence integrity and recorded replay proof; no fresh replay or inference", "source_campaign": dir, "campaign_manifest_sha256": eval.Hash(eval.Canon(manifest)), "expected_jobs": manifest["jobs"], "sealed_jobs": jobs, "sealed_shards": len(shards), "complete": jobs == int(n(manifest["jobs"])), "shards": shards}, nil
}
