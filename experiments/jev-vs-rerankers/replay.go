package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// replay creates a new analysis from immutable receipts; it never constructs an HTTP client.
func replay(from, out string) error {
	a, _ := filepath.Abs(from)
	b, _ := filepath.Abs(out)
	if a == b {
		return fmt.Errorf("replay output must differ from source")
	}
	if _, e := os.Stat(filepath.Join(out, "manifest.json")); e == nil {
		return fmt.Errorf("replay output already exists")
	}
	unlock, e := lockDir(out)
	if e != nil {
		return e
	}
	defer unlock()
	qs, _, jobs, e := evidence(from)
	if e != nil {
		return e
	}
	var original struct {
		Workers int     `json:"workers"`
		MaxCost float64 `json:"max_cost_usd"`
	}
	if e = readJSON(filepath.Join(from, "manifest.json"), &original); e != nil {
		return e
	}
	if e = frozen(out, qs, original.Workers, original.MaxCost); e != nil {
		return e
	}
	files, e := filepath.Glob(filepath.Join(from, "receipts", "*.json"))
	if e != nil {
		return e
	}
	inventory := map[string]string{}
	for _, f := range files {
		raw, e := os.ReadFile(f)
		if e != nil {
			return e
		}
		name := filepath.Base(f)
		inventory[name] = digest(raw)
		if e = atomicWrite(filepath.Join(out, "receipts", name), raw); e != nil {
			return e
		}
	}
	changed := map[string]int{}
	maxDifference := 0.0
	for _, m := range methods {
		for qi, q := range qs {
			j := jobs[m][qi]
			if !j.Success {
				return fmt.Errorf("cannot silently replay a fallback job")
			}
			before := order(j.Scores)
			v := []float64{}
			n := len(q.Candidates)
			if m == "qwen" {
				n = 1
			}
			for i := 0; i < n; i++ {
				id := j.ID
				cs := q.Candidates
				if m != "qwen" {
					id = fmt.Sprintf("%s-%03d", id, i)
					cs = q.Candidates[i : i+1]
				}
				p := payload(m, q, cs)
				found := false
				for attempt := 1; attempt <= 3; attempt++ {
					var r Receipt
					if e = readJSON(filepath.Join(from, "receipts", fmt.Sprintf("%s-%d.json", id, attempt)), &r); e != nil {
						return e
					}
					if r.PayloadSHA != digest(encode(p)) {
						return fmt.Errorf("request changed during replay")
					}
					if r.Status < 200 || r.Status >= 300 || r.Error != "" {
						continue
					}
					var body M
					if e = json.Unmarshal(r.Response, &body); e != nil {
						return e
					}
					s, e := scores(m, body, len(cs))
					if e != nil {
						return e
					}
					v = append(v, s...)
					found = true
					break
				}
				if !found {
					return fmt.Errorf("missing successful response")
				}
			}
			if len(v) != len(j.Scores) {
				return fmt.Errorf("replay cardinality mismatch")
			}
			for i := range v {
				d := math.Abs(v[i] - j.Scores[i])
				if d > maxDifference {
					maxDifference = d
				}
				if d > 1e-10 {
					return fmt.Errorf("unexpected substantive score change")
				}
			}
			j.Scores = v
			if !reflect.DeepEqual(before, order(v)) {
				changed[m]++
			}
			if e = writeJSON(filepath.Join(out, "jobs", j.ID+".json"), j); e != nil {
				return e
			}
		}
	}
	h, e := fileHash(filepath.Join(from, "manifest.json"))
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(out, "reanalysis.json"), M{"source_directory": filepath.Base(from), "source_manifest_sha256": h, "receipt_sha256": inventory, "new_http_requests": 0, "purpose": "Canonicalize expected-grade and ordinal scores to 12 decimals; preserve raw evidence and original timings.", "max_score_difference": maxDifference, "query_orders_changed": changed}); e != nil {
		return e
	}
	fmt.Printf("Offline replay complete: %d unchanged receipts; maximum numeric difference %.3g; changed orders %v.\n", len(files), maxDifference, changed)
	return nil
}

// copiedFilesMatch is used by offline evidence checks, not model execution.
func copiedFilesMatch(from, out string) error {
	files, e := filepath.Glob(filepath.Join(from, "receipts", "*.json"))
	if e != nil {
		return e
	}
	for _, p := range files {
		a, e := fileHash(p)
		if e != nil {
			return e
		}
		b, e := fileHash(filepath.Join(out, "receipts", filepath.Base(p)))
		if e != nil {
			return e
		}
		if strings.Compare(a, b) != 0 {
			return fmt.Errorf("receipt copy changed")
		}
	}
	return nil
}
