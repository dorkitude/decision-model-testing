package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPromptVariants(t *testing.T) {
	if len(promptVariants) != 9 {
		t.Fatal("expected nine variants")
	}
	q := syntheticQuery()
	for m, v := range promptVariants {
		p := payload(m, q, q.Candidates[:1])
		got, _ := json.Marshal(p["questions"])
		want, _ := json.Marshal(v)
		if string(got) != string(want) {
			t.Fatalf("variant not dispatched: %s", m)
		}
		if strings.Contains(string(got), "Paris") {
			t.Fatalf("source data leaked into instructions: %s", m)
		}
		var r M
		switch baseMethod(m) {
		case "jev-noul":
			r = M{"answers": M{"useful": M{"noul": .7}}}
		case "jev-grade":
			r = M{"answers": M{"grade": M{"probabilities": M{"irrelevant": .1, "related": .2, "useful": .3, "highly_relevant": .4}}}}
		case "jev-ordinal":
			r = M{"answers": M{"related": M{"noul": .9}, "useful": M{"noul": .7}, "direct": M{"noul": .4}}}
		default:
			t.Fatalf("bad method %s", m)
		}
		a, e := scores(m, r, 1)
		if e != nil {
			t.Fatal(e)
		}
		b, e := scores(baseMethod(m), r, 1)
		if e != nil || a[0] != b[0] {
			t.Fatalf("scoring changed: %s", m)
		}
	}
}
func TestPromptStudyPayloadGuard(t *testing.T) {
	qs, e := loadQueries()
	if e != nil {
		t.Skip(e)
	}
	for _, q := range qs {
		for _, c := range q.Candidates {
			for m := range promptVariants {
				if len(encode(payload(m, q, []Candidate{c}))) > 6000 {
					t.Fatalf("oversize %s %s", m, q.key())
				}
			}
		}
	}
}
