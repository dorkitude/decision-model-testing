package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestCategoricalDecisionsDoNotUseThreshold(t *testing.T) {
	for _, label := range []string{"direct_evidence", "supporting_evidence", "irrelevant"} {
		for _, threshold := range []float64{0, 0.5, 1} {
			d, e := filterDecision("categorical", M{"choice": label}, threshold)
			if e != nil || d["keep"] != (label != "irrelevant") || d["probability_useful"] != nil {
				t.Fatal(d, e)
			}
		}
	}
	for _, a := range []M{{"choice": "unknown"}, {"noul": 0.9}, {}} {
		if _, e := filterDecision("categorical", a, 0.5); e == nil {
			t.Fatal("invalid label accepted")
		}
	}
	for _, p := range []float64{0.49, 0.5, 0.51} {
		d, e := filterDecision("probability", M{"noul": p}, 0.5)
		if e != nil || d["keep"] != (p >= 0.5) {
			t.Fatal("probability regression")
		}
	}
}
func TestCategoricalPayloadAndWholeChunkPaging(t *testing.T) {
	h := testHarness(t)
	h.C.FilterMode = "categorical"
	units := []Unit{}
	for i := 0; i < 20; i++ {
		units = append(units, Unit{ID: fmt.Sprint(i), DocID: "doc", Text: strings.Repeat("Useful evidence. ", 250)})
	}
	pages, e := h.pages("Question?", "query", units)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, p := range pages {
		payload := h.filterPayload("Question?", "query", p)
		if !h.fitsJev(payload) {
			t.Fatal("guard exceeded")
		}
		for _, v := range obj(payload["questions"]) {
			q := obj(v)
			if q["type"] != "choice" || len(obj(q["criteria"])) != 3 {
				t.Fatal("wrong interface")
			}
		}
		for _, u := range p {
			if u != units[count] {
				t.Fatal("chunk/order changed")
			}
			count++
		}
	}
	if count != 20 {
		t.Fatal("incomplete coverage")
	}
	if filterThreshold(h.C) != nil {
		t.Fatal("categorical exposes numeric threshold")
	}
}
