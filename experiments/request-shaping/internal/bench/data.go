package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Inputs are identical to the frozen jev-vs-rerankers protocol v1.
const DatasetRevision = "39db0a25a552dd2a12012df22029686bf7fdc050"

type Source struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

var Sources = []Source{
	{"dl19.candidates.jsonl", "https://huggingface.co/datasets/castorini/rank_llm_data/resolve/" + DatasetRevision + "/retrieve_results/BM25/retrieve_results_dl19_top100.jsonl", "ec396c5725d0cec98739de9aff9fd6785da09e56fd21bee5b52dc19806f06899"},
	{"dl20.candidates.jsonl", "https://huggingface.co/datasets/castorini/rank_llm_data/resolve/" + DatasetRevision + "/retrieve_results/BM25/retrieve_results_dl20_top100.jsonl", "ab556a0cf16f868759c77db68eed99da42345e3298fd423850b8c6e0f518e0ed"},
	{"dl19.qrels", "https://trec.nist.gov/data/deep/2019qrels-pass.txt", "8a1f10d550732e4cd91d7fc49846a3784de4040972f583e69285a88f3c5fee92"},
	{"dl20.qrels", "https://trec.nist.gov/data/deep/2020qrels-pass.txt", "60d4c34561f9687f8f73e0a752ba80ab80159b3e9c1bedd0a351f747ed6f5684"},
}

// Item is one query–passage pair. Grade is -1 when unjudged.
type Item struct {
	Year      string
	QueryKey  string
	QueryText string
	DocID     string
	Text      string
	Rank      int
	Grade     int
}

func (it Item) Judged() bool   { return it.Grade >= 0 }
func (it Item) Relevant() bool { return it.Grade >= 2 }
func (it Item) Ref() string    { return it.QueryKey + "/" + it.DocID }

type Query struct {
	Year  string
	Key   string
	Text  string
	Items []Item
	Qrels map[string]int
}

type Data struct {
	Queries []Query
	ByKey   map[string]*Query
}

// Prepare fills data/ from a sibling copy when its hashes match, otherwise from the pinned URLs.
func Prepare(dir, sibling string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	for _, s := range Sources {
		path := filepath.Join(dir, s.Path)
		if h, e := FileHash(path); e == nil && h == s.SHA256 {
			continue
		}
		var b []byte
		if sibling != "" {
			if c, e := os.ReadFile(filepath.Join(sibling, s.Path)); e == nil && Digest(c) == s.SHA256 {
				b = c
			}
		}
		if b == nil {
			r, e := client.Get(s.URL)
			if e != nil {
				return e
			}
			if r.StatusCode != 200 {
				r.Body.Close()
				return fmt.Errorf("download %s: HTTP %d", s.Path, r.StatusCode)
			}
			b, e = io.ReadAll(io.LimitReader(r.Body, 32<<20))
			r.Body.Close()
			if e != nil {
				return e
			}
		}
		if Digest(b) != s.SHA256 {
			return fmt.Errorf("source hash mismatch %s", s.Path)
		}
		if e := AtomicWrite(path, b); e != nil {
			return e
		}
	}
	_, e := Load(dir)
	return e
}

func Load(dir string) (*Data, error) {
	for _, s := range Sources {
		h, e := FileHash(filepath.Join(dir, s.Path))
		if e != nil {
			return nil, fmt.Errorf("%w (run prepare)", e)
		}
		if h != s.SHA256 {
			return nil, fmt.Errorf("source hash mismatch: %s", s.Path)
		}
	}
	d := &Data{ByKey: map[string]*Query{}}
	for _, y := range []string{"dl19", "dl20"} {
		qrels, e := loadQrels(filepath.Join(dir, y+".qrels"), y)
		if e != nil {
			return nil, e
		}
		f, e := os.Open(filepath.Join(dir, y+".candidates.jsonl"))
		if e != nil {
			return nil, e
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 4096), 4<<20)
		n := 0
		for s.Scan() {
			var raw struct {
				Query struct {
					Text string `json:"text"`
					QID  int    `json:"qid"`
				} `json:"query"`
				Candidates []struct {
					DocID string  `json:"docid"`
					Score float64 `json:"score"`
					Doc   struct {
						Contents string `json:"contents"`
					} `json:"doc"`
				} `json:"candidates"`
			}
			if e = json.Unmarshal(s.Bytes(), &raw); e != nil {
				f.Close()
				return nil, e
			}
			n++
			key := y + "-" + strconv.Itoa(raw.Query.QID)
			q := Query{Year: y, Key: key, Text: raw.Query.Text, Qrels: qrels[key]}
			if len(raw.Candidates) != 100 || q.Text == "" || len(q.Qrels) == 0 {
				f.Close()
				return nil, fmt.Errorf("invalid query %s", key)
			}
			seen := map[string]bool{}
			for i, c := range raw.Candidates {
				if c.DocID == "" || c.Doc.Contents == "" || seen[c.DocID] {
					f.Close()
					return nil, fmt.Errorf("invalid candidate in %s", key)
				}
				seen[c.DocID] = true
				g, ok := q.Qrels[c.DocID]
				if !ok {
					g = -1
				}
				q.Items = append(q.Items, Item{Year: y, QueryKey: key, QueryText: q.Text, DocID: c.DocID, Text: c.Doc.Contents, Rank: i, Grade: g})
			}
			d.Queries = append(d.Queries, q)
		}
		f.Close()
		if e = s.Err(); e != nil {
			return nil, e
		}
		if want := map[string]int{"dl19": 43, "dl20": 54}[y]; n != want {
			return nil, fmt.Errorf("%s: expected %d queries, got %d", y, want, n)
		}
	}
	sort.Slice(d.Queries, func(i, j int) bool { return d.Queries[i].Key < d.Queries[j].Key })
	for i := range d.Queries {
		d.ByKey[d.Queries[i].Key] = &d.Queries[i]
	}
	return d, nil
}

func loadQrels(path, year string) (map[string]map[string]int, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	all := map[string]map[string]int{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		p := strings.Fields(l)
		if len(p) != 4 {
			return nil, fmt.Errorf("invalid qrels line")
		}
		v, e := strconv.Atoi(p[3])
		if e != nil || v < 0 || v > 3 {
			return nil, fmt.Errorf("invalid qrel grade")
		}
		k := year + "-" + p[0]
		if all[k] == nil {
			all[k] = map[string]int{}
		}
		all[k][p[2]] = v
	}
	return all, nil
}

// Subset returns a label-independent seeded sample of n queries per year.
func (d *Data) Subset(n int, seed uint64) []*Query {
	var out []*Query
	for _, y := range []string{"dl19", "dl20"} {
		var qs []*Query
		for i := range d.Queries {
			if d.Queries[i].Year == y {
				qs = append(qs, &d.Queries[i])
			}
		}
		r := RNG(seed, "subset-"+y)
		r.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })
		out = append(out, qs[:n]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (d *Data) AllItems() []Item {
	var out []Item
	for _, q := range d.Queries {
		out = append(out, q.Items...)
	}
	return out
}

// RNG derives an independent deterministic stream for each labeled use of a seed.
func RNG(seed uint64, label string) *rand.Rand {
	h := uint64(14695981039346656037)
	for _, c := range []byte(label) {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return rand.New(rand.NewPCG(seed, h))
}

func Shuffled[T any](xs []T, r *rand.Rand) []T {
	out := append([]T(nil), xs...)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
