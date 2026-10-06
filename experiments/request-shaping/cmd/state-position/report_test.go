package main

import (
	"testing"

	b "github.com/dorkitude/decision-model-testing/experiments/request-shaping/internal/bench"
)

// TestReportOnSyntheticAnswers exercises the full report with fabricated
// probabilities (grade-correlated noise); it never touches receipts or the network.
func TestReportOnSyntheticAnswers(t *testing.T) {
	d, e := b.Load("../../data")
	if e != nil {
		t.Skip("inputs not prepared")
	}
	jobs, _ := plan(d)
	r := b.RNG(1, "fake")
	var as []b.Answer
	for _, j := range jobs {
		n := map[string]float64{}
		for _, dd := range j.Decisions {
			n[dd.Key] = float64(int(100*(0.2*float64(max(dd.Grade, 0))+0.4*r.Float64()))) / 100
		}
		for _, k := range j.Extra {
			n[k] = r.Float64()
		}
		as = append(as, b.Answer{Job: j, Nouls: n, Tokens: len(j.Body) / 4, Seconds: 0.1, Attempts: 1, OK: true})
	}
	if e := report(d, as, t.TempDir()); e != nil {
		t.Fatal(e)
	}
}
