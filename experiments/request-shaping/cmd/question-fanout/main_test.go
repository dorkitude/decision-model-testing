package main

import (
	"fmt"
	"os"
	"testing"

	b "github.com/dorkitude/decision-model-testing/experiments/request-shaping/internal/bench"
)

func TestFrozenRequestsReproduced(t *testing.T) {
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
	for _, i := range []int{0, 42, 96} {
		q := d.Queries[i]
		it := q.Items[i]
		for method, j := range map[string]b.Job{"jev-ordinal": ordinalJoint("x", it), "jev-noul": job("x", "solo", it, []b.KV{useful})} {
			if e := b.ReadJSON(fmt.Sprintf("../../%s/%s-%s-%03d-1.json", b.FrozenRun, method, q.Key, i), &r); e != nil {
				t.Fatal(e)
			}
			if b.Digest(j.Body) != r.PayloadSHA {
				t.Fatalf("%s %s item %d differs from frozen request", method, q.Key, i)
			}
		}
	}
}

func TestFanShapes(t *testing.T) {
	if len(relatedPool) != 15 || len(unrelatedPool) != 15 {
		t.Fatal("pools must hold 15 questions")
	}
	f := fan(unrelatedPool, 16, true)
	if len(f) != 16 || f[15].K != "useful" || fan(relatedPool, 3, false)[0].K != "useful" {
		t.Fatal("bad fan order")
	}
}
