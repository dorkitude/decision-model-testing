package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const datasetRevision = "39db0a25a552dd2a12012df22029686bf7fdc050"
const trecRevision = "ba38899cbd4de0fb699b47f39b64ef1c107e4a5c"

type Source struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

var sources = []Source{
	{"data/dl19.candidates.jsonl", "https://huggingface.co/datasets/castorini/rank_llm_data/resolve/" + datasetRevision + "/retrieve_results/BM25/retrieve_results_dl19_top100.jsonl", "ec396c5725d0cec98739de9aff9fd6785da09e56fd21bee5b52dc19806f06899"},
	{"data/dl20.candidates.jsonl", "https://huggingface.co/datasets/castorini/rank_llm_data/resolve/" + datasetRevision + "/retrieve_results/BM25/retrieve_results_dl20_top100.jsonl", "ab556a0cf16f868759c77db68eed99da42345e3298fd423850b8c6e0f518e0ed"},
	{"data/dl19.qrels", "https://trec.nist.gov/data/deep/2019qrels-pass.txt", "8a1f10d550732e4cd91d7fc49846a3784de4040972f583e69285a88f3c5fee92"},
	{"data/dl20.qrels", "https://trec.nist.gov/data/deep/2020qrels-pass.txt", "60d4c34561f9687f8f73e0a752ba80ab80159b3e9c1bedd0a351f747ed6f5684"},
}

type Candidate struct {
	DocID string  `json:"docid"`
	Score float64 `json:"score"`
	Doc   struct {
		Contents string `json:"contents"`
	} `json:"doc"`
}
type Query struct {
	Year  string `json:"year"`
	Query struct {
		Text string `json:"text"`
		QID  int    `json:"qid"`
	} `json:"query"`
	Candidates []Candidate `json:"candidates"`
}

func (q Query) key() string { return fmt.Sprintf("%s-%d", q.Year, q.Query.QID) }
func verifySources() error {
	for _, s := range sources {
		h, e := fileHash(s.Path)
		if e != nil {
			return e
		}
		if h != s.SHA256 {
			return fmt.Errorf("source hash mismatch: %s", s.Path)
		}
	}
	return nil
}
func prepare() error {
	client := &http.Client{Timeout: 3 * time.Minute}
	for _, s := range sources {
		if _, e := os.Stat(s.Path); os.IsNotExist(e) {
			r, e := client.Get(s.URL)
			if e != nil {
				return e
			}
			if r.StatusCode != 200 {
				r.Body.Close()
				return fmt.Errorf("download %s: HTTP %d", s.Path, r.StatusCode)
			}
			b, e := io.ReadAll(io.LimitReader(r.Body, 32<<20))
			r.Body.Close()
			if e != nil {
				return e
			}
			if digest(b) != s.SHA256 {
				return fmt.Errorf("download hash mismatch %s", s.Path)
			}
			if e = atomicWrite(s.Path, b); e != nil {
				return e
			}
		}
	}
	if e := verifySources(); e != nil {
		return e
	}
	qs, e := loadQueries()
	if e != nil {
		return e
	}
	rels, e := loadQrels()
	if e != nil {
		return e
	}
	for _, q := range qs {
		if len(rels[q.key()]) == 0 {
			return fmt.Errorf("missing qrels %s", q.key())
		}
	}
	if len(rels) != len(qs) {
		return fmt.Errorf("qrels/candidate query count mismatch")
	}
	rev, e := exec.Command("git", "-C", "third_party/trec_eval", "rev-parse", "HEAD").Output()
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(rev)) != trecRevision {
		return fmt.Errorf("trec_eval revision mismatch")
	}
	cmd := exec.Command("make", "-s", "-C", "third_party/trec_eval")
	if b, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("build trec_eval: %s: %w", b, e)
	}
	maxBytes := 0
	for _, q := range qs {
		for _, c := range q.Candidates {
			for _, m := range methods[1:] {
				p := payload(m, q, []Candidate{c})
				n := len(encode(p))
				if n > maxBytes {
					maxBytes = n
				}
				if n > 6000 {
					return fmt.Errorf("Jev input exceeds conservative 6000-byte guard")
				}
			}
		}
	}
	manifest := M{"sources": sources, "dataset_revision": datasetRevision, "trec_eval_revision": trecRevision, "queries": len(qs), "candidates_per_query": 100, "max_jev_payload_utf8_bytes": maxBytes, "text_truncation": false}
	if e := writeJSON("data/manifest.json", manifest); e != nil {
		return e
	}
	fmt.Printf("Prepared %d queries / %d candidates, max Jev payload %d bytes; built pinned trec_eval.\n", len(qs), len(qs)*100, maxBytes)
	return nil
}
func loadQueries() ([]Query, error) {
	if e := verifySources(); e != nil {
		return nil, e
	}
	var qs []Query
	seen := map[string]bool{}
	for _, y := range []string{"dl19", "dl20"} {
		f, e := os.Open("data/" + y + ".candidates.jsonl")
		if e != nil {
			return nil, e
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 4096), 4<<20)
		n := 0
		for s.Scan() {
			var q Query
			if e = json.Unmarshal(s.Bytes(), &q); e != nil {
				f.Close()
				return nil, e
			}
			q.Year = y
			n++
			if seen[q.key()] || q.Query.Text == "" || len(q.Candidates) != 100 {
				f.Close()
				return nil, fmt.Errorf("invalid query %s", q.key())
			}
			seen[q.key()] = true
			ids := map[string]bool{}
			last := 1e100
			for _, c := range q.Candidates {
				if c.DocID == "" || c.Doc.Contents == "" || ids[c.DocID] || c.Score > last {
					f.Close()
					return nil, fmt.Errorf("invalid candidate or non-descending BM25 in %s", q.key())
				}
				ids[c.DocID] = true
				last = c.Score
			}
			qs = append(qs, q)
		}
		f.Close()
		if e = s.Err(); e != nil {
			return nil, e
		}
		expected := 43
		if y == "dl20" {
			expected = 54
		}
		if n != expected {
			return nil, fmt.Errorf("%s: expected %d queries", y, expected)
		}
	}
	sort.Slice(qs, func(i, j int) bool {
		if qs[i].Year != qs[j].Year {
			return qs[i].Year < qs[j].Year
		}
		return qs[i].Query.QID < qs[j].Query.QID
	})
	return qs, nil
}
func loadQrels() (map[string]map[string]int, error) {
	all := map[string]map[string]int{}
	for _, y := range []string{"dl19", "dl20"} {
		b, e := os.ReadFile("data/" + y + ".qrels")
		if e != nil {
			return nil, e
		}
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			p := strings.Fields(l)
			if len(p) != 4 {
				return nil, fmt.Errorf("invalid qrels")
			}
			v, e := strconv.Atoi(p[3])
			if e != nil || v < 0 || v > 3 {
				return nil, fmt.Errorf("invalid qrel grade")
			}
			k := y + "-" + p[0]
			if all[k] == nil {
				all[k] = map[string]int{}
			}
			if _, ok := all[k][p[2]]; ok {
				return nil, fmt.Errorf("duplicate qrel")
			}
			all[k][p[2]] = v
		}
	}
	return all, nil
}
