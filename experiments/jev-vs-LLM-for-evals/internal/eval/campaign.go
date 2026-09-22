package eval

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// CampaignPlan packs matched model groups and entire tie cohorts into bounded
// shards. This avoids loading a multi-GB request archive into one process.
func CampaignPlan(root, out string, config, pricing M) (manifest M, err error) {
	defer Recover(&err)
	Validate(config)
	check(Prepare(root))
	jobs := Plan(root, config)
	VerifyJobs(jobs)
	units := map[string][]M{}
	counts := M{}
	for _, j := range jobs {
		r := obj(j["row"])
		id := str(r["id"])
		if r["subset"] == "Ties" {
			_, id, _ = strings.Cut(id, ":")
		}
		unit := strings.Join([]string{str(j["benchmark"]), str(j["method"]), str(r["subset"]), id}, "\t")
		units[unit] = append(units[unit], j)
		b := str(j["benchmark"])
		counts[b] = num(counts[b]) + 1
	}
	keys := []string{}
	for k := range units {
		keys = append(keys, k)
	}
	rank := func(k string) string { return Hash(Canon([]any{"campaign-v1", config["seed"], k})) }
	sort.Slice(keys, func(i, j int) bool { return rank(keys[i]) < rank(keys[j]) })
	limit := int(num(config["shard_jobs"]))
	if limit < 1 {
		limit = 100
	}
	byteLimit := 16 << 20
	shards := []M{}
	batch := []M{}
	size := 0
	flush := func() {
		if len(batch) == 0 {
			return
		}
		sort.Slice(batch, func(i, j int) bool { return rank(str(batch[i]["key"])) < rank(str(batch[j]["key"])) })
		name := fmt.Sprintf("shards/%04d", len(shards))
		path := filepath.Join(out, name, "jobs.json")
		raw := Canon(batch)
		if _, e := os.Stat(path); e == nil {
			if !bytes.Equal(Canon(ReadJSON(path)), raw) {
				panic("campaign shard changed")
			}
		} else if _, e := os.Stat(path + ".gz"); e == nil {
			if !bytes.Equal(Canon(ReadJSON(path)), raw) {
				panic("archived campaign shard changed")
			}
		} else {
			WriteJSON(path, batch)
		}
		shards = append(shards, M{"directory": name, "jobs": len(batch), "jobs_sha256": Hash(raw)})
		batch = nil
		size = 0
	}
	for _, k := range keys {
		unit := units[k]
		n := len(Canon(unit))
		if len(batch) > 0 && (len(batch)+len(unit) > limit || size+n > byteLimit) {
			flush()
		}
		batch = append(batch, unit...)
		size += n
	}
	flush()
	studyID := str(config["study_id"])
	if studyID == "" {
		studyID = "broad-published-v1"
	}
	manifest = M{"schema_version": 1, "study": studyID, "config": config, "pricing": pricing, "source_sha256": SourceManifest(root), "jobs": len(jobs), "matched_units": len(units), "benchmark_jobs": counts, "shards": shards, "schedule": "sha256-v1 randomized matched units and model order; all ref/tied companions in one shard"}
	path := filepath.Join(out, "campaign.json")
	if _, e := os.Stat(path); e == nil {
		if !bytes.Equal(Canon(ReadJSON(path)), Canon(manifest)) {
			panic("campaign manifest changed")
		}
	} else {
		WriteJSON(path, manifest)
	}
	return
}
func CampaignRun(ctx context.Context, root, out string) (err error) {
	defer Recover(&err)
	manifest := obj(ReadJSON(filepath.Join(out, "campaign.json")))
	if !bytes.Equal(Canon(SourceManifest(root)), Canon(manifest["source_sha256"])) {
		panic("campaign source changed; use its frozen checkout")
	}
	f, e := os.OpenFile(filepath.Join(out, "campaign.lock"), os.O_CREATE|os.O_RDWR, 0644)
	check(e)
	defer f.Close()
	check(syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	config, pricing := obj(manifest["config"]), obj(manifest["pricing"])
	meter := NewMeter(config, pricing)
	shards := arr(manifest["shards"])
	for _, v := range shards {
		dir := filepath.Join(out, str(obj(v)["directory"]))
		for _, name := range []string{"requests.jsonl", "reservations.jsonl", "results.jsonl", "job-timings.jsonl"} {
			RepairTail(filepath.Join(dir, name))
		}
		meter.Restore(dir)
	}
	completed := 0
	finishedJobs := 0
	for i, v := range shards {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		spec := obj(v)
		dir := filepath.Join(out, str(spec["directory"]))
		status := M{}
		if _, e := os.Stat(filepath.Join(dir, "status-go.json")); e == nil {
			status = obj(ReadJSON(filepath.Join(dir, "status-go.json")))
		}
		if !yes(status["complete"]) {
			raw := arr(ReadJSON(filepath.Join(dir, "jobs.json")))
			jobs := []M{}
			for _, j := range raw {
				jobs = append(jobs, obj(j))
			}
			if Hash(Canon(jobs)) != spec["jobs_sha256"] {
				panic("campaign jobs hash changed")
			}
			WriteJSON(filepath.Join(out, "progress.json"), M{"complete": false, "active_shard": i, "completed_shards": completed, "total_shards": len(shards), "finished_jobs": finishedJobs, "expected_jobs": manifest["jobs"], "budget": meter.Snapshot(), "updated_utc": time.Now().UTC().Format(time.RFC3339Nano)})
			if e := runMeter(ctx, root, dir, config, jobs, pricing, meter); e != nil {
				WriteJSON(filepath.Join(out, "failure.json"), M{"shard": i, "error": e.Error(), "budget": meter.Snapshot()})
				return e
			}
		}
		// Verify payload/result replay before sealing each newly finished shard.
		proof := M{}
		if _, e := os.Stat(filepath.Join(dir, "go-replay-verification.json")); e == nil {
			proof = obj(ReadJSON(filepath.Join(dir, "go-replay-verification.json")))
		}
		if !yes(proof["complete"]) || num(proof["replayed_jobs"]) != num(spec["jobs"]) {
			verified, e := Replay(root, dir)
			check(e)
			if !yes(verified["complete"]) {
				panic("shard replay incomplete")
			}
		}
		SealShard(dir)
		completed++
		finishedJobs += int(num(spec["jobs"]))
		WriteJSON(filepath.Join(out, "progress.json"), M{"complete": completed == len(shards), "completed_shards": completed, "total_shards": len(shards), "finished_jobs": finishedJobs, "expected_jobs": manifest["jobs"], "budget": meter.Snapshot(), "updated_utc": time.Now().UTC().Format(time.RFC3339Nano)})
		fmt.Printf("CAMPAIGN %d/%d shards, %d/%v jobs, estimated committed $%.4f\n", completed, len(shards), finishedJobs, manifest["jobs"], num(meter.Snapshot()["committed_estimated_usd"]))
	}
	return
}
