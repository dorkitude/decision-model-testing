package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestScoreSemantics(t *testing.T) {
	tests := []struct {
		m    string
		r    M
		want []float64
	}{
		{"jev-noul", M{"answers": M{"useful": M{"noul": .73}}}, []float64{.73}},
		{"jev-grade", M{"answers": M{"grade": M{"probabilities": M{"irrelevant": .1, "related": .2, "useful": .3, "highly_relevant": .4}}}}, []float64{2}},
		{"jev-ordinal", M{"answers": M{"related": M{"noul": .9}, "useful": M{"noul": .7}, "direct": M{"noul": .4}}}, []float64{2}},
		{"qwen", M{"data": []any{M{"index": float64(1), "relevance_score": .9}, M{"index": float64(0), "relevance_score": .2}}}, []float64{.2, .9}},
	}
	for _, tt := range tests {
		n := 1
		if tt.m == "qwen" {
			n = 2
		}
		got, e := scores(tt.m, tt.r, n)
		if e != nil {
			t.Fatal(e)
		}
		for i := range got {
			if math.Abs(got[i]-tt.want[i]) > 1e-12 {
				t.Fatalf("%s: %v", tt.m, got)
			}
		}
	}
	for _, r := range []M{{"answers": M{}}, {"answers": M{"useful": M{"noul": 1.01}}}, {"answers": M{"useful": M{"noul": nil}}}} {
		if _, e := scores("jev-noul", r, 1); e == nil {
			t.Fatal("invalid probability accepted")
		}
	}
	if _, e := scores("qwen", M{"data": []any{M{"index": float64(0), "relevance_score": .2}, M{"index": float64(0), "relevance_score": .9}}}, 2); e == nil {
		t.Fatal("duplicate index accepted")
	}
	if !reflect.DeepEqual(order([]float64{.5, .9, .5}), []int{1, 0, 2}) {
		t.Fatal("tie order changed")
	}
}
func TestPayloadHasOnlyQuestionAndPassage(t *testing.T) {
	q := syntheticQuery()
	for _, m := range methods[1:] {
		p := payload(m, q, q.Candidates[:1])
		state := obj(p["state"])
		if len(state) != 2 || state["question"] != q.Query.Text || state["passage"] != q.Candidates[0].Doc.Contents {
			t.Fatal("unexpected state")
		}
		if strings.Contains(string(encode(p)), "qid") {
			t.Fatal("ID leak")
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReceiptResumeAndBudget(t *testing.T) {
	out := t.TempDir()
	c, e := newClient(out, 1)
	if e != nil {
		t.Fatal(e)
	}
	c.keys = map[string]string{"TYPESAFE_TOKEN": "test-secret"}
	var count atomic.Int32
	c.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"answers":{"useful":{"noul":0.9}},"usage":{"input_tokens":100},"model":"test"}`)), Header: make(http.Header)}, nil
	})}
	q := syntheticQuery()
	p := payload("jev-noul", q, q.Candidates[:1])
	for i := 0; i < 2; i++ {
		if _, e = c.call("test", "jev-noul", p); e != nil {
			t.Fatal(e)
		}
	}
	if count.Load() != 1 {
		t.Fatal("repeated cached call")
	}
	p["state"] = M{"question": "changed", "passage": "different"}
	if _, e = c.call("test", "jev-noul", p); e == nil {
		t.Fatal("changed identity accepted")
	}
	b, _ := os.ReadFile(filepath.Join(out, "receipts/test-1.json"))
	if strings.Contains(string(b), "test-secret") {
		t.Fatal("credential saved")
	}
	c2, e := newClient(out, 1e-12)
	if e != nil {
		t.Fatal(e)
	}
	c2.keys = c.keys
	c2.HTTP = c.HTTP
	if _, e = c2.call("other", "jev-noul", p); e == nil {
		t.Fatal("budget ignored")
	}
	if count.Load() != 1 {
		t.Fatal("budget dispatched")
	}
	if e = writeJSON(filepath.Join(out, "reservations/pending-1.json"), Receipt{UpperUSD: .001}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.call("pending", "jev-noul", p); e == nil {
		t.Fatal("ambiguous call repeated")
	}
}
func TestConcurrentPassageReplay(t *testing.T) {
	c, _ := newClient(t.TempDir(), 1)
	c.keys = map[string]string{"TYPESAFE_TOKEN": "test"}
	var count atomic.Int32
	c.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"answers":{"useful":{"noul":0.6}},"usage":{"input_tokens":1}}`)), Header: make(http.Header)}, nil
	})}
	q := syntheticQuery()
	j, e := execute(c, q, "jev-noul", 3)
	if e != nil {
		t.Fatal(e)
	}
	if len(j.Scores) != 3 || !j.Success {
		t.Fatal(j)
	}
	j2, e := execute(c, q, "jev-noul", 3)
	if e != nil || !reflect.DeepEqual(j, j2) || count.Load() != 3 {
		t.Fatal("query resume failed", e, count.Load())
	}
}
func TestMetricParityOfficial(t *testing.T) {
	if _, e := os.Stat("third_party/trec_eval/trec_eval"); e != nil {
		t.Skip("run prepare for public scorer integration", e)
	}
	q := syntheticQuery()
	q.Candidates[0].DocID = "a"
	q.Candidates[1].DocID = "b"
	q.Candidates[2].DocID = "c"
	rels := map[string]int{"a": 1, "b": 3, "c": 0, "missing": 2}
	m := metrics(q, []float64{3, 2, 1}, rels)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "qrels"), []byte("1 0 a 1\n1 0 b 3\n1 0 c 0\n1 0 missing 2\n"), 0600)
	os.WriteFile(filepath.Join(dir, "run"), []byte("1 Q0 a 1 3 test\n1 Q0 b 2 2 test\n1 Q0 c 3 1 test\n"), 0600)
	for _, tc := range []struct {
		metric string
		want   float64
		extra  []string
	}{{"ndcg_cut.10", m.NDCG, nil}, {"recip_rank", m.MRR, []string{"-l", "2", "-M", "10"}}, {"recall.10", m.Recall10, []string{"-l", "2"}}} {
		args := append([]string{"-m", tc.metric}, tc.extra...)
		args = append(args, filepath.Join(dir, "qrels"), filepath.Join(dir, "run"))
		b, e := exec.Command("third_party/trec_eval/trec_eval", args...).Output()
		if e != nil {
			t.Fatal(e)
		}
		var metric, key string
		var got float64
		if _, e = fmt.Sscan(string(b), &metric, &key, &got); e != nil || math.Abs(got-tc.want) > .000051 {
			t.Fatalf("metric parity %s: %s / %g", tc.metric, b, tc.want)
		}
	}
	if m.MRR != .5 || m.Recall10 != .5 {
		t.Fatal("binary grade semantics or full-qrels denominator wrong")
	}
}
func TestPreparedInputs(t *testing.T) {
	if _, e := os.Stat("data/dl19.candidates.jsonl"); os.IsNotExist(e) {
		t.Skip("run prepare for public-data integration")
	}
	qs, e := loadQueries()
	if e != nil {
		t.Fatal(e)
	}
	if len(qs) != 97 {
		t.Fatal("not full benchmark")
	}
	rels, e := loadQrels()
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range qs {
		if len(rels[q.key()]) == 0 {
			t.Fatal("missing query")
		}
		for _, c := range q.Candidates {
			for _, m := range methods[1:] {
				b := encode(payload(m, q, []Candidate{c}))
				if len(b) > 6000 || !json.Valid(b) {
					t.Fatal("input context guard")
				}
			}
		}
	}
}

func TestCanonicalDecimalTies(t *testing.T) {
	a := M{"answers": M{"related": M{"noul": .1}, "useful": M{"noul": .2}, "direct": M{"noul": .3}}}
	b := M{"answers": M{"related": M{"noul": .3}, "useful": M{"noul": .2}, "direct": M{"noul": .1}}}
	x, e := scores("jev-ordinal", a, 1)
	if e != nil {
		t.Fatal(e)
	}
	y, e := scores("jev-ordinal", b, 1)
	if e != nil {
		t.Fatal(e)
	}
	if x[0] != y[0] || x[0] != .6 {
		t.Fatal("decimal ties not canonical", x, y)
	}
}

func TestOfflineReplayAndDiagnosticReport(t *testing.T) {
	if _, e := os.Stat("data/dl19.candidates.jsonl"); os.IsNotExist(e) {
		t.Skip("run prepare for public-data integration")
	}
	qs, e := loadQueries()
	if e != nil {
		t.Fatal(e)
	}
	q := qs[0]
	from := t.TempDir()
	out := t.TempDir()
	if e = frozen(from, qs[:1], 2, 1); e != nil {
		t.Fatal(e)
	}
	c, e := newClient(from, 1)
	if e != nil {
		t.Fatal(e)
	}
	c.keys = map[string]string{"TYPESAFE_TOKEN": "test", "FIREWORKS_API_KEY": "test"}
	var count atomic.Int32
	c.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		var p M
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			return nil, e
		}
		response := M{"usage": M{"input_tokens": 10}, "model": "synthetic-test"}
		if _, ok := p["documents"]; ok {
			a := []any{}
			for i := 0; i < 100; i++ {
				a = append(a, M{"index": i, "relevance_score": float64(100 - i)})
			}
			response["data"] = a
		} else {
			a := M{}
			for k, x := range obj(p["questions"]) {
				if obj(x)["type"] == "choice" {
					a[k] = M{"probabilities": M{"irrelevant": .1, "related": .2, "useful": .3, "highly_relevant": .4}}
				} else {
					a[k] = M{"noul": .5}
				}
			}
			response["answers"] = a
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(encode(response)))), Header: make(http.Header)}, nil
	})}
	for _, m := range methods {
		if _, e = execute(c, q, m, 2); e != nil {
			t.Fatal(e)
		}
	}
	if count.Load() != 301 {
		t.Fatal(count.Load())
	}
	if e = replay(from, out); e != nil {
		t.Fatal(e)
	}
	if e = copiedFilesMatch(from, out); e != nil {
		t.Fatal(e)
	}
	if count.Load() != 301 {
		t.Fatal("replay called provider")
	}
	if e = report(out); e != nil {
		t.Fatal(e)
	}
	if e = audit(out); e != nil {
		t.Fatal(e)
	}
	c2, e := newClient(out, 1)
	if e != nil {
		t.Fatal(e)
	}
	if c2.reserved <= 0 {
		t.Fatal("imported receipts not counted against budget")
	}
}
