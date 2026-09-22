package study

import "testing"

func TestJointlyValidComparisonExcludesEitherFailure(t *testing.T) {
	rows := cascadeFixture()
	for _, r := range rows {
		if r["case_id"] == "0" && r["model"] == "jev-latest" {
			r["valid"] = false
			r["error_attempts"] = M{"HTTP 429": 3.}
		}
		if r["case_id"] == "1" && r["model"] != "jev-latest" {
			r["valid"] = false
		}
	}
	got := Reliability(rows, 20)
	for _, r := range got["jointly_valid_comparisons"].([]M) {
		if n(r["matched_units"]) != 60 || n(r["jointly_valid_units"]) != 58 {
			t.Fatal(r)
		}
	}
	for _, r := range got["all_attempt_reliability"].([]M) {
		if r["model"] == "jev-latest" && n(m(r["error_attempts"])["HTTP 429"]) != 3 {
			t.Fatal("lost failed/retried attempts", r)
		}
	}
}
