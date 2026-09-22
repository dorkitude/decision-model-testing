package study

import (
	"math"
	"testing"
)

func TestCalibrationFirstOrderCoverageAndBoundaries(t *testing.T) {
	rows := []M{}
	for i, p := range []any{0., 1., .5, nil} {
		rows = append(rows, M{"benchmark": "llmbar", "method": "Vanilla", "model": "jev-latest", "valid": true, "label": "1", "choice_p_original_a": []any{p, 0.}, "case_id": i})
	}
	got := Calibration(rows)[0]
	if n(got["eligible"]) != 3 || n(got["coverage"]) != .75 || math.Abs(n(got["binary_brier"])-1.25/3) > 1e-12 {
		t.Fatal(got)
	}
	bins := got["bins"].([]M)
	if n(bins[0]["n"]) != 1 || n(bins[9]["n"]) != 1 {
		t.Fatal("boundary probability assigned wrong bin")
	}
}
