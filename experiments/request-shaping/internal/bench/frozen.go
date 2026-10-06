package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FrozenRun locates the jev-vs-rerankers trec-v1 receipts (2026-09-18) used for drift checks.
const FrozenRun = "../jev-vs-rerankers/results/trec-v1/receipts"

type FrozenAnswer struct {
	Nouls      map[string]float64
	PayloadSHA string
	Started    string
}

// Frozen returns the successful trec-v1 answers of one method keyed by item ref.
// It returns nil when the private receipts are unavailable.
func Frozen(d *Data, method string, keys []string) (map[string]FrozenAnswer, error) {
	if _, e := os.Stat(FrozenRun); e != nil {
		return nil, nil
	}
	out := map[string]FrozenAnswer{}
	for _, q := range d.Queries {
		for i, it := range q.Items {
			for attempt := 1; attempt <= 3; attempt++ {
				var r struct {
					PayloadSHA string          `json:"payload_sha256"`
					Status     int             `json:"status"`
					Error      string          `json:"error"`
					Started    string          `json:"started_utc"`
					Response   json.RawMessage `json:"response"`
				}
				path := filepath.Join(FrozenRun, fmt.Sprintf("%s-%s-%03d-%d.json", method, q.Key, i, attempt))
				if e := ReadJSON(path, &r); e != nil {
					break
				}
				if r.Status == 200 && r.Error == "" {
					n, _, e := ParseNouls(r.Response, keys)
					if e != nil {
						return nil, e
					}
					out[it.Ref()] = FrozenAnswer{n, r.PayloadSHA, r.Started}
					break
				}
			}
		}
	}
	return out, nil
}
