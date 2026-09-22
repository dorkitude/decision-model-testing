package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Job struct {
	ID       string    `json:"id"`
	Method   string    `json:"method"`
	QueryKey string    `json:"query_key"`
	DocIDs   []string  `json:"docids"`
	Scores   []float64 `json:"scores"`
	Started  string    `json:"started_utc"`
	Seconds  float64   `json:"query_wall_seconds"`
	Success  bool      `json:"success"`
	Failure  string    `json:"failure,omitempty"`
	Reused   int       `json:"reused_requests"`
}

func order(v []float64) []int {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return v[idx[i]] > v[idx[j]] })
	return idx
}
func frozen(out string, qs []Query, workers int, maxCost float64) error {
	h, e := sourceHash()
	if e != nil {
		return e
	}
	ids := []string{}
	for _, q := range qs {
		ids = append(ids, q.key())
	}
	manifest := M{"version": "trec-v1", "source_sha256": h, "sources": sources, "dataset_revision": datasetRevision, "trec_eval_revision": trecRevision, "query_keys": ids, "candidate_depth": 100, "methods": methods, "jev_model": jevModel, "qwen_model": qwenModel, "workers": workers, "max_cost_usd": maxCost, "seed": 20260918, "bootstrap_replicates": 10000, "rates_usd_per_million": M{"jev_input": .042, "qwen_input": .2}, "methods_payload_examples": M{}}
	examples := manifest["methods_payload_examples"].(M)
	q := syntheticQuery()
	for _, m := range methods {
		examples[m] = payload(m, q, q.Candidates[:1])
	}
	path := filepath.Join(out, "manifest.json")
	if b, e := os.ReadFile(path); e == nil {
		if string(b) != string(encode(manifest)) {
			return fmt.Errorf("frozen manifest differs; use a new output directory")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	return writeJSON(path, manifest)
}
func syntheticQuery() Query {
	var q Query
	q.Year = "synthetic"
	q.Query.QID = 1
	q.Query.Text = "What is the capital of France?"
	for i, s := range []string{"Paris is the capital of France.", "France is a country in Europe.", "Whales are marine mammals."} {
		var c Candidate
		c.DocID = fmt.Sprint(i)
		c.Doc.Contents = s
		q.Candidates = append(q.Candidates, c)
	}
	return q
}
func smoke(out string, maxCost float64) error {
	unlock, e := lockDir(out)
	if e != nil {
		return e
	}
	defer unlock()
	q := syntheticQuery()
	if e = frozen(out, []Query{q}, 1, maxCost); e != nil {
		return e
	}
	c, e := newClient(out, maxCost)
	if e != nil {
		return e
	}
	for _, m := range methods {
		if len(methods) > 4 {
			if _, ok := promptVariants[m]; !ok {
				continue
			}
		}
		j, e := execute(c, q, m, 1)
		if e != nil {
			return e
		}
		if !j.Success || j.Scores[0] <= j.Scores[2] {
			return fmt.Errorf("smoke relevance ordering failed for %s", m)
		}
		fmt.Printf("%s: scores %v; direct answer outranks unrelated passage\n", m, j.Scores)
	}
	fmt.Printf("Smoke complete; new HTTP requests: %d\n", c.calls)
	return nil
}
func execute(c *Client, q Query, method string, workers int) (Job, error) {
	id := method + "-" + q.key()
	path := filepath.Join(c.Out, "jobs", id+".json")
	var j Job
	if e := readJSON(path, &j); e == nil {
		return j, nil
	} else if !os.IsNotExist(e) {
		return j, e
	}
	j = Job{ID: id, Method: method, QueryKey: q.key(), Started: time.Now().UTC().Format(time.RFC3339Nano), Scores: make([]float64, len(q.Candidates)), Success: true}
	for _, x := range q.Candidates {
		j.DocIDs = append(j.DocIDs, x.DocID)
	}
	start := time.Now()
	cached := 0
	files, e := filepath.Glob(filepath.Join(c.Out, "receipts", id+"-*.json"))
	if e != nil {
		return j, e
	}
	seenCached := map[string]bool{}
	for _, f := range files {
		var r Receipt
		if e := readJSON(f, &r); e != nil {
			return j, e
		}
		if r.Status >= 200 && r.Status < 300 && r.Error == "" {
			key := r.ID[:len(r.ID)-2]
			if !seenCached[key] {
				cached++
				seenCached[key] = true
			}
		}
	}
	if method == "qwen" {
		r, e := c.call(id, method, payload(method, q, q.Candidates))
		if errors.Is(e, errTransient) {
			j.Success = false
			j.Failure = e.Error()
		} else if e != nil {
			return j, e
		} else {
			j.Scores, e = scores(method, r, len(q.Candidates))
			if e != nil {
				return j, e
			}
		}
	} else {
		indices := make(chan int, len(q.Candidates))
		for i := range q.Candidates {
			indices <- i
		}
		close(indices)
		errs := make(chan error, len(q.Candidates))
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range indices {
					rid := fmt.Sprintf("%s-%03d", id, i)
					r, e := c.call(rid, method, payload(method, q, q.Candidates[i:i+1]))
					if e == nil {
						var v []float64
						v, e = scores(method, r, 1)
						if e == nil {
							j.Scores[i] = v[0]
						}
					}
					if e != nil {
						errs <- e
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if !errors.Is(e, errTransient) {
				return j, e
			}
			j.Success = false
			j.Failure = e.Error()
		}
	}
	if !j.Success {
		for i := range j.Scores {
			j.Scores[i] = float64(len(j.Scores) - i)
		}
	}
	j.Seconds = time.Since(start).Seconds()
	expected := len(q.Candidates)
	if method == "qwen" {
		expected = 1
	}
	j.Reused = min(expected, cached)
	if e := writeJSON(path, j); e != nil {
		return j, e
	}
	return j, nil
}
func runBenchmark(out string, workers, limit int, maxCost float64) error {
	if workers < 1 || workers > 64 || limit < 0 {
		return fmt.Errorf("workers must be 1..64; limit nonnegative")
	}
	unlock, e := lockDir(out)
	if e != nil {
		return e
	}
	defer unlock()
	qs, e := loadQueries()
	if e != nil {
		return e
	}
	if limit > 0 && limit < len(qs) {
		qs = qs[:limit]
	}
	if e = verifyBaselineImport(out); e != nil {
		return e
	}
	if e = frozen(out, qs, workers, maxCost); e != nil {
		return e
	}
	c, e := newClient(out, maxCost)
	if e != nil {
		return e
	}
	if len(methods) > 4 {
		var imported M
		if e := readJSON(filepath.Join(out, "baseline-import.json"), &imported); e != nil {
			return fmt.Errorf("seed-baseline is required: %w", e)
		}
		for _, q := range qs {
			for _, m := range baselineMethods {
				var j Job
				if e := readJSON(filepath.Join(out, "jobs", m+"-"+q.key()+".json"), &j); e != nil || !j.Success {
					return fmt.Errorf("missing successful baseline job %s/%s", m, q.key())
				}
			}
		}
	}
	start := time.Now()
	// Rotate method order deterministically by query to distribute time-of-day effects.
	for i, q := range qs {
		for m := range methods {
			method := methods[(i+m)%len(methods)]
			j, e := execute(c, q, method, workers)
			if e != nil {
				return e
			}
			fmt.Printf("query %d/%d %s %-12s success=%v seconds=%.2f requests_this_session=%d\n", i+1, len(qs), q.key(), method, j.Success, j.Seconds, c.calls)
		}
	}
	fmt.Printf("Completed %d queries x %d methods; new HTTP requests %d; session %.1fs\n", len(qs), len(methods), c.calls, time.Since(start).Seconds())
	return nil
}
