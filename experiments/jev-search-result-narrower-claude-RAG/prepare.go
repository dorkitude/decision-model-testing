package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkoukk/tiktoken-go"
)

var tokenizer *tiktoken.Tiktoken
var tokenOnce sync.Once
var tokenErr error
var tokenMu sync.Mutex

func tokens(s string) []int {
	tokenOnce.Do(func() { tokenizer, tokenErr = tiktoken.GetEncoding("cl100k_base") })
	if tokenErr != nil {
		panic(tokenErr)
	}
	tokenMu.Lock()
	defer tokenMu.Unlock()
	return tokenizer.Encode(s, nil, nil)
}
func tokenCount(v any) int { return len(tokens(string(canon(v)))) }
func unitsFor(docID, text string) []Unit {
	ids := tokens(text)
	offsets := make([]int, len(ids)+1)
	tokenMu.Lock()
	for i, id := range ids {
		offsets[i+1] = offsets[i] + len(tokenizer.Decode([]int{id}))
	}
	tokenMu.Unlock()
	var out []Unit
	for start := 0; start < len(ids); {
		end := min(start+1024, len(ids))
		for end < len(ids) && !utf8.ValidString(text[offsets[start]:offsets[end]]) {
			end++
		}
		s := text[offsets[start]:offsets[end]]
		id := hash([]byte("chunks1024" + docID + strconv.Itoa(offsets[start]) + s))
		out = append(out, Unit{id, docID, s, offsets[start], offsets[end]})
		if end == len(ids) {
			break
		}
		next := end - 1024/5
		if next <= start {
			next = end
		}
		start = next
		for start < end && !utf8.ValidString(text[offsets[start]:offsets[end]]) {
			start++
		}
	}
	return out
}
func (h *Harness) prepare() error {
	if e := os.MkdirAll("data", 0700); e != nil {
		return e
	}
	if e := h.prepareSample(); e != nil {
		return e
	}
	if _, e := h.questions(); e != nil {
		return e
	}
	if _, e := os.Stat("data/units.jsonl"); os.IsNotExist(e) {
		b, e := h.cli("documents", "export", "--data-dir", h.C.DataDir)
		if e != nil {
			return e
		}
		// The source export is processed only to reconstruct existing chunk text/IDs; no embedding calls.
		tmp := "data/units.jsonl.tmp"
		f, e := os.Create(tmp)
		if e != nil {
			return e
		}
		w := bufio.NewWriter(f)
		docs := 0
		count := 0
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var d M
			if e = json.Unmarshal([]byte(line), &d); e != nil {
				f.Close()
				return e
			}
			text := str(d["email"])
			if text == "" {
				text = str(d["text"])
			}
			did := str(d["document_id"])
			if did == "" || text == "" {
				f.Close()
				return fmt.Errorf("empty document export")
			}
			docs++
			for _, u := range unitsFor(did, text) {
				if _, e = w.Write(append(canon(u), '\n')); e != nil {
					f.Close()
					return e
				}
				count++
			}
			if docs%10000 == 0 {
				fmt.Printf("reconstructed documents=%d units=%d\n", docs, count)
			}
		}
		if e = w.Flush(); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		if docs != 73772 || count != 84375 {
			return fmt.Errorf("corpus mismatch: documents=%d units=%d", docs, count)
		}
		if e = os.Rename(tmp, "data/units.jsonl"); e != nil {
			return e
		}
		if e = jsonFile("data/corpus.json", M{"documents": docs, "units": count, "dataset_revision": revision, "chunk_tokens": 1024, "overlap_tokens": 204, "tokenizer": "cl100k_base", "chunk_id": "sha256(chunks1024 + document_id + start_byte_decimal + text)"}); e != nil {
			return e
		}
	}
	if e := h.loadUnits(); e != nil {
		return e
	}
	// Reuse the audited lexical namespace from the original study without rewriting it.
	for _, ns := range []string{h.C.DenseNS, h.C.LexicalNS} {
		r, e := h.call("count/"+ns, "index-audit", "https://"+h.C.Region+".turbopuffer.com/v2/namespaces/"+ns+"/query", "TURBOPUFFER_API_KEY", M{"aggregate_by": M{"count": []any{"Count"}}, "consistency": M{"level": "strong"}})
		if e != nil {
			return e
		}
		count := num(obj(r["aggregations"])["count"])
		if int(count) != len(h.Units) {
			return fmt.Errorf("namespace %s count %g != %d", ns, count, len(h.Units))
		}
	}
	fmt.Printf("prepared %d questions, %d reused vector IDs and matching lexical chunks\n", h.C.Questions, len(h.Units))
	return nil
}
func (h *Harness) loadUnits() error {
	h.Units = map[string]Unit{}
	return scan("data/units.jsonl", func(b []byte) error {
		var u Unit
		if e := json.Unmarshal(b, &u); e != nil {
			return e
		}
		if u.ID == "" || u.Text == "" {
			return fmt.Errorf("invalid unit")
		}
		if _, ok := h.Units[u.ID]; ok {
			return fmt.Errorf("duplicate unit")
		}
		h.Units[u.ID] = u
		return nil
	})
}
func (h *Harness) embed(key, query string) ([]float64, error) {
	r, e := h.call(key, "embedding", fw+"/embeddings", "FIREWORKS_API_KEY", M{"model": h.C.Embedding, "input": []string{"Instruct: Given a question about workplace emails, retrieve passages that answer the question.\nQuery: " + query}})
	if e != nil {
		return nil, e
	}
	data := arr(r["data"])
	if len(data) != 1 {
		return nil, fmt.Errorf("embedding count")
	}
	v := arr(obj(data[0])["embedding"])
	if len(v) != 4096 {
		return nil, fmt.Errorf("embedding dimension %d", len(v))
	}
	out := make([]float64, len(v))
	var norm float64
	for i, x := range v {
		out[i] = num(x)
		norm += out[i] * out[i]
	}
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, fmt.Errorf("invalid embedding norm")
	}
	for i := range out {
		out[i] /= math.Sqrt(norm)
	}
	return out, nil
}
func (h *Harness) preflight() error {
	catalog, e := h.call("catalog/"+time.Now().UTC().Format("2006-01-02"), "preflight", fw+"/models", "FIREWORKS_API_KEY", nil)
	if e != nil {
		return e
	}
	available := map[string]bool{}
	for _, v := range arr(catalog["data"]) {
		available[str(obj(v)["id"])] = true
	}
	for _, model := range []string{h.C.DeepSeek, h.C.Reranker, strings.Replace(h.C.Embedding, "fireworks/", "accounts/fireworks/models/", 1)} {
		if !available[model] {
			return fmt.Errorf("model absent from live catalog: %s", model)
		}
	}
	if e = h.S.put("models", M{"id": time.Now().UTC().Format("2006-01-02"), "catalog": catalog, "qa": h.C.QA, "deepseek": h.C.DeepSeek, "jev": h.C.Jev}); e != nil {
		return e
	}
	for _, model := range []string{h.C.DeepSeek} {
		r, e := h.call("probe/"+model, "preflight", fw+"/chat/completions", "FIREWORKS_API_KEY", M{"model": model, "messages": []M{{"role": "user", "content": "Return only READY."}}, "max_tokens": 256, "temperature": 0, "reasoning_effort": map[bool]string{true: "low", false: "none"}[model == h.C.QA]})
		if e != nil {
			return e
		}
		if len(arr(r["choices"])) != 1 {
			return fmt.Errorf("chat probe failed")
		}
	}
	_, e = h.call("probe/rerank", "preflight", fw+"/rerank", "FIREWORKS_API_KEY", M{"model": h.C.Reranker, "query": "When is the meeting?", "documents": []string{"Meeting at noon.", "Bananas are yellow."}, "top_n": 2})
	if e != nil {
		return e
	}
	r, e := h.call("probe/jev", "preflight", jevURL, "TYPESAFE_TOKEN", M{"model": h.C.Jev, "state": "The meeting is at noon.", "questions": M{"correct": M{"type": "noul", "instructions": "Is the meeting at noon?"}}})
	if e != nil {
		return e
	}
	if num(obj(obj(r["answers"])["correct"])["noul"]) < .5 {
		return fmt.Errorf("Jev synthetic probe failed")
	}

	if e = h.claudePreflight(); e != nil {
		return e
	}
	_, e = h.embed("probe/embedding", "When is the meeting?")
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join("data", "questions.jsonl")); e == nil {
		qs, e := h.questions()
		if e != nil {
			return e
		}
		fmt.Printf("sample ready: %d\n", len(qs))
	}
	fmt.Println("provider access checks passed")
	return nil
}
