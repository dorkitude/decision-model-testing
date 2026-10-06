package main

import (
	"os"
	"testing"

	b "github.com/dorkitude/decision-model-testing/experiments/request-shaping/internal/bench"
)

func TestAtomicMatchesFrozenRequests(t *testing.T) {
	d, e := b.Load("../../data")
	if e != nil {
		t.Skip("inputs not prepared")
	}
	if _, e := os.Stat("../../" + b.FrozenRun); e != nil {
		t.Skip("private frozen receipts unavailable")
	}
	var r struct {
		PayloadSHA string `json:"payload_sha256"`
	}
	for _, i := range []int{0, 57, 96} {
		q := d.Queries[i]
		if e := b.ReadJSON("../../"+b.FrozenRun+"/jev-noul-"+q.Key+"-"+pad(i)+"-1.json", &r); e != nil {
			t.Fatal(e)
		}
		if got := b.Digest(atomic("x", "atomic", q.Items[i]).Body); got != r.PayloadSHA {
			t.Fatalf("%s item %d: payload %s differs from frozen %s", q.Key, i, got, r.PayloadSHA)
		}
	}
}

func pad(i int) string {
	return string([]byte{byte('0' + i/100), byte('0' + i/10%10), byte('0' + i%10)})
}

func TestPlanShapes(t *testing.T) {
	d, e := b.Load("../../data")
	if e != nil {
		t.Skip("inputs not prepared")
	}
	jobs, _ := plan(d)
	seen := map[string]bool{}
	for _, j := range jobs {
		if seen[j.ID] {
			t.Fatalf("duplicate job id %s", j.ID)
		}
		seen[j.ID] = true
		qs := map[string]bool{}
		for _, dd := range j.Decisions {
			if j.Arm[:5] == "cross" {
				if qs[dd.Query] {
					t.Fatalf("%s repeats a query", j.ID)
				}
				qs[dd.Query] = true
			}
		}
	}
	for _, k := range packSizes {
		for _, q := range d.Queries {
			n := 0
			for _, p := range packs(&q, k, 1) {
				n += len(p)
			}
			if n != 100 {
				t.Fatalf("pack %d covers %d items", k, n)
			}
		}
	}
}
