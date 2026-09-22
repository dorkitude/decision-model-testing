package study

import "testing"

func TestNativeComparisonMatchesCasesAndRejectsChangedAnswers(t *testing.T) {
	row := func(method, id, hash string, score, cost float64) M {
		return M{"benchmark": "llmbar", "model": "jev-latest", "method": method, "case_id": id, "subset": "Natural", "comparison_content_sha256": hash, "valid": true, "published_score": score, "accounting_usd": cost}
	}
	primary := []M{row("Vanilla", "1", "same", 0, 2), row("Vanilla", "2", "unmatched", 1, 3)}
	native := []M{row("Compact", "1", "same", 1, 1), row("Atomic", "1", "same", 1, 4)}
	got := NativeComparisons(primary, native, 20)
	if len(got) != 3 || n(got[0]["matched_units"]) != 1 || n(m(got[0]["paired"])["published_delta"]) != 1 || n(m(got[0]["paired"])["accounting_cost_ratio_a_over_b"]) != .5 {
		t.Fatal(got)
	}
	native[0]["comparison_content_sha256"] = "changed"
	defer func() {
		if recover() == nil {
			t.Error("changed comparison content accepted")
		}
	}()
	NativeComparisons(primary, native, 20)
}
