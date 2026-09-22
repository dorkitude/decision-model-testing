package eval

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func Validate(config M) {
	for _, key := range []string{"concurrency", "max_requests", "max_tokens_floor", "per_subset", "shard_jobs"} {
		v := num(config[key])
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v != math.Trunc(v) {
			panic(fmt.Errorf("%s must be a nonnegative integer", key))
		}
	}
	if v := num(config["max_estimated_usd"]); v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		panic("invalid estimated cost cap")
	}

	if num(config["concurrency"]) < 1 || num(config["max_requests"]) < 1 || num(config["timeout_s"]) <= 0 || len(arr(config["models"])) == 0 {
		panic("positive concurrency, max_requests, timeout_s and at least one model are required")
	}
	if num(config["per_subset"]) < 0 || num(config["max_tokens_floor"]) < 0 {
		panic("negative limits")
	}
	if mode := str(config["jev_rating_mode"]); mode != "modal" && mode != "expected" {
		panic("jev_rating_mode must be modal or expected")
	}
}
func SourceManifest(root string) M {
	files := []string{"go.mod", "go.sum", "methodology/sources.lock.json"}
	for _, dir := range []string{"internal/eval", "cmd/evalevaluation"} {
		names, e := filepath.Glob(filepath.Join(root, dir, "*.go"))
		check(e)
		for _, p := range names {
			r, e := filepath.Rel(root, p)
			check(e)
			files = append(files, r)
		}
	}
	hashes := M{}
	for _, f := range files {
		hashes[f] = Hash(Read(filepath.Join(root, f)))
	}
	return hashes
}
func VerifyJobs(jobs []M) {
	keys := map[string]bool{}
	for _, j := range jobs {
		k := str(j["key"])
		if k == "" || keys[k] {
			panic("missing/duplicate job key")
		}
		keys[k] = true
		row := obj(j["row"])
		expected := Hash(Canon([]any{j["benchmark"], j["method"], j["model"], row["subset"], row["id"]}))[:24]
		if k != expected {
			panic("job identity hash mismatch")
		}
		if len(obj(j["row"])) == 0 {
			panic("missing row")
		}
		b, m := str(j["benchmark"]), str(j["method"])
		if b != "llmbar" && !(b == "judgebench" && (m == "vanilla" || m == "arena_hard")) && !(b == "rewardbench2" && (m == "fourway" || m == "ratings")) {
			panic("unknown benchmark or method")
		}
	}
}
func Summarize(out string) M {
	jobs := arr(ReadJSON(filepath.Join(out, "jobs.json")))
	results := Lines(filepath.Join(out, "results.jsonl"))
	wanted, seen := map[string]bool{}, map[string]bool{}
	for _, j := range jobs {
		wanted[str(obj(j)["key"])] = true
	}
	for _, r := range results {
		k := str(r["key"])
		if seen[k] || !wanted[k] {
			panic("unexpected/duplicate result")
		}
		seen[k] = true
	}
	complete := len(seen) == len(wanted)
	s := M{"complete": complete, "finished": len(seen), "expected": len(wanted)}
	if complete {
		s["scores"] = Scores(results)
	}
	WriteJSON(filepath.Join(out, "summary-go.json"), s)
	return s
}
func Run(ctx context.Context, root, out string, config M, jobs []M, pricing M) error {
	return runMeter(ctx, root, out, config, jobs, pricing, nil)
}
func runMeter(ctx context.Context, root, out string, config M, jobs []M, pricing M, shared *Meter) (err error) {
	defer Recover(&err)
	Validate(config)
	VerifyJobs(jobs)
	check(Prepare(root))
	check(os.MkdirAll(out, 0755))
	lock, e := os.OpenFile(filepath.Join(out, "run.lock"), os.O_CREATE|os.O_RDWR, 0644)
	check(e)
	defer lock.Close()
	check(syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	manifest := M{"engine": "go-cobra-viper-v1", "config": config, "jobs_sha256": Hash(Canon(jobs)), "jobs": len(jobs), "source_sha256": SourceManifest(root), "pricing_sha256": Hash(Canon(pricing))}
	path := filepath.Join(out, "manifest.json")
	if _, e := os.Stat(path); e == nil {
		varOld := ReadJSON(path)
		if !bytes.Equal(Canon(varOld), Canon(manifest)) {
			panic("manifest changed; use a new output directory")
		}
		if !bytes.Equal(Canon(ReadJSON(filepath.Join(out, "jobs.json"))), Canon(jobs)) {
			panic("frozen jobs changed")
		}
	} else if os.IsNotExist(e) {
		WriteJSON(path, manifest)
		WriteJSON(filepath.Join(out, "jobs.json"), jobs)
		WriteJSON(filepath.Join(out, "pricing.snapshot.json"), pricing)
	} else {
		check(e)
	}
	for _, name := range []string{"requests.jsonl", "reservations.jsonl", "results.jsonl", "job-timings.jsonl"} {
		RepairTail(filepath.Join(out, name))
	}
	done := map[string]bool{}
	for _, r := range Lines(filepath.Join(out, "results.jsonl")) {
		done[str(r["key"])] = true
	}
	c := NewClient(out, config, false)
	c.Context = ctx
	if shared != nil {
		c.Meter = shared
	}
	queue := []M{}
	for _, j := range jobs {
		if !done[str(j["key"])] {
			queue = append(queue, j)
		}
	}
	var mu sync.Mutex
	next := 0
	stopped := false
	errs := []string{}
	var wg sync.WaitGroup
	for i := 0; i < int(num(config["concurrency"])); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if stopped || next >= len(queue) || ctx.Err() != nil {
					mu.Unlock()
					return
				}
				j := queue[next]
				next++
				mu.Unlock()
				start := time.Now()
				result, e := Evaluate(root, j, c)
				duration := time.Since(start).Seconds()
				mu.Lock()
				Append(filepath.Join(out, "job-timings.jsonl"), M{"key": j["key"], "started_utc": start.UTC().Format(time.RFC3339Nano), "active_wall_s": duration, "completed": e == nil})
				if e != nil {
					stopped = true
					errs = append(errs, e.Error())
				} else {
					Append(filepath.Join(out, "results.jsonl"), result)
					done[str(j["key"])] = true
					fmt.Printf("%d/%d %s %s %s valid=%v\n", len(done), len(jobs), j["benchmark"], j["method"], filepath.Base(str(j["model"])), result["valid"])
				}
				if len(done)%25 == 0 || e != nil {
					WriteJSON(filepath.Join(out, "progress.json"), M{"finished": len(done), "expected": len(jobs), "updated_utc": time.Now().UTC().Format(time.RFC3339Nano), "budget": c.Meter.Snapshot(), "errors": errs})
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	s := Summarize(out)
	s["errors"] = errs
	s["budget"] = c.Meter.Snapshot()
	WriteJSON(filepath.Join(out, "status-go.json"), s)
	_, e = Economics(out, pricing)
	check(e)
	if !yes(s["complete"]) {
		return fmt.Errorf("run incomplete: %d/%d jobs; %v", len(done), len(jobs), errs)
	}
	return
}
func Replay(root, out string) (report M, err error) {
	defer Recover(&err)
	check(Prepare(root))
	manifest := obj(ReadJSON(filepath.Join(out, "manifest.json")))
	config := obj(manifest["config"])
	jobs := arr(ReadJSON(filepath.Join(out, "jobs.json")))
	byKey := map[string]M{}
	for _, j := range jobs {
		byKey[str(obj(j)["key"])] = obj(j)
	}
	c := NewClient(out, config, true)
	results := Lines(filepath.Join(out, "results.jsonl"))
	seen := map[string]bool{}
	for _, old := range results {
		key := str(old["key"])
		if seen[key] {
			panic("duplicate result")
		}
		seen[key] = true
		j, ok := byKey[key]
		if !ok {
			panic("orphan result")
		}
		r, e := Evaluate(root, j, c)
		check(e)
		// Canonical round-trip normalizes []M and int into JSON types before comparison.
		a, b := decode(Canon(r)), decode(Canon(old))
		if !Equivalent(a, b) {
			WriteJSON(filepath.Join(out, "replay-mismatch.json"), M{"key": key, "got": r, "wanted": old})
			panic(fmt.Errorf("result replay mismatch %s", key))
		}
	}
	report = M{"engine": "go-cobra-viper-v1", "replayed_jobs": len(results), "expected_jobs": len(jobs), "complete": len(results) == len(jobs), "identical_results": true, "identical_payloads": true, "replayed_stages": len(c.seen), "inference_calls": 0, "go_source_sha256": SourceManifest(root)}
	WriteJSON(filepath.Join(out, "go-replay-verification.json"), report)
	return
}
