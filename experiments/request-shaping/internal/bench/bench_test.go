package bench

import (
	"math"
	"testing"
)

func TestObjKeepsOrder(t *testing.T) {
	got := string(Encode(Obj{P("z", 1), P("a", M{"y": 2, "b": 3})}))
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"b\": 3,\n    \"y\": 2\n  }\n}\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestAUC(t *testing.T) {
	xs := []Scored{{Grade: 3, P: .9}, {Grade: 2, P: .5}, {Grade: 0, P: .5}, {Grade: 1, P: .1}}
	if v := AUC(xs); math.Abs(v-0.875) > 1e-12 {
		t.Fatalf("AUC %v", v)
	}
	if !math.IsNaN(AUC(xs[:2])) {
		t.Fatal("AUC without negatives must be NaN")
	}
}

func TestNDCG10(t *testing.T) {
	q := &Query{Qrels: map[string]int{"a": 3, "b": 1, "z": 2}}
	for i, id := range []string{"b", "a", "c"} {
		q.Items = append(q.Items, Item{DocID: id, Rank: i})
	}
	// Tie between a and b keeps BM25 order (b first); z is judged but not retrieved.
	got := NDCG10(q, map[string]float64{"a": .5, "b": .5, "c": .1})
	idcg := 3 + 2/math.Log2(3) + 1/math.Log2(4)
	want := (1 + 3/math.Log2(3)) / idcg
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("nDCG %v want %v", got, want)
	}
}

func TestHolm(t *testing.T) {
	got := Holm([]float64{0.01, 0.04, 0.03})
	want := []float64{0.03, 0.06, 0.06}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("Holm %v", got)
		}
	}
}

func TestClusterBootDeterministic(t *testing.T) {
	qs := []string{"dl19-1", "dl19-2", "dl20-1", "dl20-2"}
	v := map[string]float64{"dl19-1": 1, "dl19-2": 2, "dl20-1": 3, "dl20-2": 4}
	stat := func(s []string) float64 {
		t := 0.0
		for _, q := range s {
			t += v[q]
		}
		return t / float64(len(s))
	}
	a := ClusterBoot(qs, YearOf, stat, 500, 1, "x")
	b := ClusterBoot(qs, YearOf, stat, 500, 1, "x")
	if a != b || a.Est != 2.5 || a.Lo > 2.5 || a.Hi < 2.5 {
		t.Fatalf("bootstrap %v %v", a, b)
	}
}

func TestParseNoulsRejectsMissing(t *testing.T) {
	if _, _, e := ParseNouls([]byte(`{"model":"jev-1.13.0","answers":{"a":{"noul":0.2}}}`), []string{"a", "b"}); e == nil {
		t.Fatal("missing key accepted")
	}
	n, _, e := ParseNouls([]byte(`{"model":"jev-1.13.0","answers":{"a":{"noul":0}}}`), []string{"a"})
	if e != nil || n["a"] != 0 {
		t.Fatal("zero probability rejected")
	}
}

// TestNDCGMatchesFrozenTrecEval checks NDCG10 against the trec_eval-verified
// per-query values of the frozen jev-vs-rerankers run (private evidence).
func TestNDCGMatchesFrozenTrecEval(t *testing.T) {
	d, e := Load("../../data")
	if e != nil {
		t.Skip("inputs not prepared")
	}
	var rows []struct {
		Query   string `json:"query_key"`
		Method  string `json:"method"`
		Metrics struct {
			NDCG float64 `json:"ndcg_at_10"`
		} `json:"metrics"`
	}
	if e := ReadJSON("../../../jev-vs-rerankers/results/trec-v1/per-query.json", &rows); e != nil {
		t.Skip("frozen run unavailable")
	}
	n := 0
	for _, r := range rows {
		if r.Method != "jev-noul" {
			continue
		}
		var job struct {
			DocIDs []string  `json:"docids"`
			Scores []float64 `json:"scores"`
		}
		if e := ReadJSON("../../../jev-vs-rerankers/results/trec-v1/jobs/jev-noul-"+r.Query+".json", &job); e != nil {
			t.Fatal(e)
		}
		s := map[string]float64{}
		for i, id := range job.DocIDs {
			s[id] = job.Scores[i]
		}
		if got := NDCG10(d.ByKey[r.Query], s); math.Abs(got-r.Metrics.NDCG) > 1e-9 {
			t.Fatalf("%s: nDCG %v, frozen %v", r.Query, got, r.Metrics.NDCG)
		}
		n++
	}
	if n != 97 {
		t.Fatalf("checked %d queries", n)
	}
}
