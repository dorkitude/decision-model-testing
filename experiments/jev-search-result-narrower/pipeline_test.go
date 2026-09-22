package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testHarness(t *testing.T) *Harness {
	t.Helper()
	s, e := newStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return &Harness{C: Config{Jev: "jev-1.13.0", JevPageTokens: 6000, JevPageBytes: 24000, JevMaxQuestions: 6, Threshold: .5, MaxRequests: 10}, S: s, Ctx: context.Background(), HTTP: http.DefaultClient}
}
func TestPagesRespectWholeRequestAndCoverExactly(t *testing.T) {
	h := testHarness(t)
	units := []Unit{}
	for i := 0; i < 20; i++ {
		units = append(units, Unit{ID: fmt.Sprint(i), DocID: "doc", Text: strings.Repeat("東京 <evidence> ", 200)})
	}
	pages, e := h.pages("What happened?", "search", units)
	if e != nil {
		t.Fatal(e)
	}
	if len(pages) < 2 {
		t.Fatal("did not page")
	}
	flat := []Unit{}
	for _, p := range pages {
		payload := h.filterPayload("What happened?", "search", p)
		if !h.fitsJev(payload) {
			t.Fatal("oversized page")
		}
		flat = append(flat, p...)
	}
	if !reflect.DeepEqual(flat, units) {
		t.Fatal("lost, reordered or truncated results")
	}
	payload := h.filterPayload("What happened?", "search", units[:1])
	h.C.JevPageBytes = len(canon(payload))
	h.C.JevPageTokens = tokenCount(payload)
	if !h.fitsJev(payload) {
		t.Fatal("exact boundary rejected")
	}
	h.C.JevPageBytes--
	if h.fitsJev(payload) {
		t.Fatal("byte boundary ignored")
	}
	if _, e = h.pages("What happened?", "search", units[:1]); e == nil {
		t.Fatal("oversized singleton silently accepted")
	}
	h.C.JevPageBytes = 100000
	h.C.JevPageTokens = tokenCount(payload) - 1
	if h.fitsJev(payload) {
		t.Fatal("token boundary ignored")
	}
}
func TestStoreAndHTTPResume(t *testing.T) {
	h := testHarness(t)
	t.Setenv("TEST_JEV_KEY", "synthetic-secret")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			t.Error("missing auth")
		}
		fmt.Fprint(w, `{"answer":true}`)
	}))
	defer srv.Close()
	p := M{"units": []string{"a", "b"}, "n": 2}
	if _, e := h.call("test", "test", srv.URL, "TEST_JEV_KEY", p); e != nil {
		t.Fatal(e)
	}
	s, e := newStore(h.S.Dir)
	if e != nil {
		t.Fatal(e)
	}
	h.S = s
	if _, e = h.call("test", "test", srv.URL, "TEST_JEV_KEY", p); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("resume repeated inference")
	}
	b, e := os.ReadFile(filepath.Join(h.S.Dir, "requests.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "synthetic-secret") {
		t.Fatal("credential leaked")
	}
	if e = h.S.put("example", M{"id": "one", "ids": []string{"x"}, "count": 1}); e != nil {
		t.Fatal(e)
	}
	r, _ := h.S.get("example", "one")
	if num(r["count"]) != 1 || len(arr(r["ids"])) != 1 {
		t.Fatal("JSON types differ before restart")
	}
	if e = h.S.put("example", M{"id": "one", "count": 2}); e == nil {
		t.Fatal("mutated evidence")
	}
}

type routeTransport struct{ endpoint string }

func (rt routeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	u := *r.URL
	req.URL = &u
	parsed, _ := http.NewRequest("POST", rt.endpoint, nil)
	req.URL.Scheme = parsed.URL.Scheme
	req.URL.Host = parsed.URL.Host
	return http.DefaultTransport.RoundTrip(req)
}
func TestFilterPagesAndPreservesOrder(t *testing.T) {
	h := testHarness(t)
	h.C.JevMaxQuestions = 2
	t.Setenv("TYPESAFE_TOKEN", "synthetic-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p M
		json.NewDecoder(r.Body).Decode(&p)
		state := obj(p["state"])
		if state["original_question"] != "original" {
			t.Error("missing question")
		}
		answers := M{}
		for _, v := range arr(state["results"]) {
			row := obj(v)
			prob := .5
			if row["chunk_id"] == "b" {
				prob = .1
			}
			answers[str(row["result_id"])] = M{"noul": prob}
		}
		json.NewEncoder(w).Encode(M{"answers": answers})
	}))
	defer srv.Close()
	h.HTTP = &http.Client{Transport: routeTransport{srv.URL}}
	units := []Unit{{ID: "a", Text: "A"}, {ID: "b", Text: "B"}, {ID: "c", Text: "C"}}
	h.Units = map[string]Unit{}
	for _, u := range units {
		h.Units[u.ID] = u
	}
	got, id, e := h.filter(Question{ID: "q", Question: "original", DocID: "gold-hidden"}, Search{ID: "s", Query: "query", Results: units})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, []Unit{units[0], units[2]}) {
		t.Fatal(got)
	}
	r, _ := h.S.get("filters", id)
	if num(r["pages"]) != 2 || len(arr(r["decisions"])) != 3 {
		t.Fatal("page coverage")
	}
	for _, req := range h.S.all("requests") {
		if strings.Contains(string(canon(req["payload"])), "gold-hidden") {
			t.Fatal("gold leakage")
		}
	}
	got2, _, e := h.filter(Question{ID: "q"}, Search{ID: "s"})
	if e != nil || !reflect.DeepEqual(got, got2) {
		t.Fatal("filter resume differs")
	}
}
func TestRRFDeduplicatesAndBreaksTies(t *testing.T) {
	got := fuse([]string{"a", "b", "b"}, []string{"b", "a"}, 10)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatal(got)
	}
}
