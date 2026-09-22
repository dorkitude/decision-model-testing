package study

import (
	"path/filepath"
	"testing"
)

func TestAtomicReceiptsRetainRubricAnswers(t *testing.T) {
	t.Skip("integration requires upstream data or private historical fixtures; excluded from source-free offline suite")
	rows, _, err := LoadRun(filepath.Join("..", "..", "results", "native-interface-smoke-v1"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range rows {
		if r["method"] != "Atomic" {
			continue
		}
		count++
		orders := a(r["native_orders"])
		if len(orders) != 2 {
			t.Fatal("lost order")
		}
		for _, v := range orders {
			order := m(v)
			if len(m(order["answers"])) != 6 {
				t.Fatal("lost atomic answers")
			}
			if r["model"] == "jev-latest" && len(m(order["noul_probabilities"])) != 6 {
				t.Fatal("lost Noul probabilities")
			}
		}
	}
	if count != 5 || len(AtomicDiagnostics(rows)) != 5 {
		t.Fatal("missing model coverage")
	}
}
