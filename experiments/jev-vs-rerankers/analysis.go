package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Metrics struct {
	NDCG        float64 `json:"ndcg_at_10"`
	MRR         float64 `json:"mrr_at_10"`
	Recall10    float64 `json:"recall_at_10"`
	Recall100   float64 `json:"recall_at_100"`
	Judged      float64 `json:"candidate_judged_fraction"`
	TieFraction float64 `json:"tie_fraction"`
}

func metrics(q Query, values []float64, rels map[string]int) Metrics {
	var m Metrics
	idx := order(values)
	ideal := []int{}
	total := 0
	for _, r := range rels {
		ideal = append(ideal, r)
		if r >= 2 {
			total++
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	idcg := 0.0
	for i, r := range ideal {
		if i >= 10 {
			break
		}
		idcg += float64(r) / math.Log2(float64(i+2))
	}
	unique := map[float64]bool{}
	for i, k := range idx {
		c := q.Candidates[k]
		r := rels[c.DocID]
		unique[values[k]] = true
		if _, ok := rels[c.DocID]; ok {
			m.Judged++
		}
		if i < 10 {
			m.NDCG += float64(r) / math.Log2(float64(i+2))
			if r >= 2 {
				m.Recall10++
				if m.MRR == 0 {
					m.MRR = 1 / float64(i+1)
				}
			}
		}
		if r >= 2 {
			m.Recall100++
		}
	}
	if idcg > 0 {
		m.NDCG /= idcg
	}
	if total > 0 {
		m.Recall10 /= float64(total)
		m.Recall100 /= float64(total)
	}
	m.Judged /= float64(len(idx))
	m.TieFraction = 1 - float64(len(unique))/float64(len(idx))
	return m
}

type EvalRow struct {
	Year     string  `json:"year"`
	QueryKey string  `json:"query_key"`
	Method   string  `json:"method"`
	Metrics  Metrics `json:"metrics"`
	Seconds  float64 `json:"query_wall_seconds"`
	Success  bool    `json:"success"`
	Reused   int     `json:"reused_requests"`
}
type Resource struct {
	MissingUsageUpperUSD float64        `json:"missing_usage_upper_usd"`
	Requests             int            `json:"requests"`
	Failed               int            `json:"failed_requests"`
	MissingUsage         int            `json:"missing_usage_requests"`
	InputTokens          float64        `json:"input_tokens"`
	OutputTokens         float64        `json:"output_tokens"`
	USD                  float64        `json:"list_price_usd"`
	UpperUSD             float64        `json:"reserved_upper_usd"`
	RequestSeconds       []float64      `json:"request_seconds"`
	Models               map[string]int `json:"resolved_models"`
}

func receiptResource(r Receipt) (float64, float64, bool) {
	var resp M
	if json.Unmarshal(r.Response, &resp) != nil {
		return 0, 0, false
	}
	u := obj(resp["usage"])
	input, ok := number(u["input_tokens"])
	if !ok {
		input, ok = number(u["prompt_tokens"])
	}
	output, _ := number(u["output_tokens"])
	if _, yes := u["output_tokens"]; !yes {
		output, _ = number(u["completion_tokens"])
	}
	return input, output, ok
}
func resources(out string) (map[string]*Resource, error) {
	all := map[string]*Resource{}
	for _, m := range methods {
		all[m] = &Resource{Models: map[string]int{}}
	}
	files, e := filepath.Glob(filepath.Join(out, "receipts", "*.json"))
	if e != nil {
		return nil, e
	}
	for _, f := range files {
		var r Receipt
		if e = readJSON(f, &r); e != nil {
			return nil, e
		}
		v := all[r.Method]
		if v == nil {
			return nil, fmt.Errorf("unknown method in receipt")
		}
		v.Requests++
		v.UpperUSD += r.UpperUSD
		v.RequestSeconds = append(v.RequestSeconds, r.Seconds)
		if r.Error != "" || r.Status < 200 || r.Status >= 300 {
			v.Failed++
		}
		in, op, ok := receiptResource(r)
		if !ok {
			v.MissingUsage++
			v.MissingUsageUpperUSD += r.UpperUSD
		} else {
			v.InputTokens += in
			v.OutputTokens += op
			rate := .042
			if r.Method == "qwen" {
				rate = .2
			}
			cost := in * rate / 1e6
			if cost > r.UpperUSD+1e-9 {
				return nil, fmt.Errorf("billing exceeds reservation: %s", r.ID)
			}
			v.USD += cost
		}
		var body M
		json.Unmarshal(r.Response, &body)
		if model, ok := body["model"].(string); ok {
			v.Models[model]++
		}
	}
	return all, nil
}
func percentile(a []float64, p float64) float64 {
	if len(a) == 0 {
		return 0
	}
	b := append([]float64{}, a...)
	sort.Float64s(b)
	return b[int(math.Round(p*float64(len(b)-1)))]
}
func mean(a []float64) float64 {
	s := 0.0
	for _, v := range a {
		s += v
	}
	if len(a) == 0 {
		return 0
	}
	return s / float64(len(a))
}
func bootstrap(a, b []EvalRow) (float64, float64, float64) {
	if len(a) == 0 {
		return 0, 0, 0
	}
	groups := map[string][]float64{}
	for i := range a {
		groups[a[i].Year] = append(groups[a[i].Year], a[i].Metrics.NDCG-b[i].Metrics.NDCG)
	}
	rng := rand.New(rand.NewSource(20260918))
	samples := make([]float64, 10000)
	delta := 0.0
	for _, y := range []string{"dl19", "dl20"} {
		for _, d := range groups[y] {
			delta += d
		}
	}
	for i := range samples {
		s := 0.0
		for _, y := range []string{"dl19", "dl20"} {
			g := groups[y]
			for range g {
				s += g[rng.Intn(len(g))]
			}
		}
		samples[i] = s / float64(len(a))
	}
	return delta / float64(len(a)), percentile(samples, .025), percentile(samples, .975)
}
func evidence(out string) ([]Query, []EvalRow, map[string][]Job, error) {
	qs, e := loadQueries()
	if e != nil {
		return nil, nil, nil, e
	}
	var manifest struct {
		QueryKeys []string `json:"query_keys"`
		Methods   []string `json:"methods"`
	}
	if e = readJSON(filepath.Join(out, "manifest.json"), &manifest); e != nil {
		return nil, nil, nil, e
	}
	if strings.Join(manifest.Methods, "\x00") != strings.Join(methods, "\x00") {
		return nil, nil, nil, fmt.Errorf("method roster differs: use the matching --study")
	}
	want := map[string]bool{}
	for _, k := range manifest.QueryKeys {
		want[k] = true
	}
	filtered := []Query{}
	for _, q := range qs {
		if want[q.key()] {
			filtered = append(filtered, q)
		}
	}
	if len(filtered) != len(manifest.QueryKeys) {
		return nil, nil, nil, fmt.Errorf("manifest query mismatch")
	}
	qs = filtered
	rels, e := loadQrels()
	if e != nil {
		return nil, nil, nil, e
	}
	rows := []EvalRow{}
	jobs := map[string][]Job{}
	for _, q := range qs {
		bm := make([]float64, len(q.Candidates))
		oracle := make([]float64, len(q.Candidates))
		for i, c := range q.Candidates {
			bm[i] = c.Score
			oracle[i] = float64(rels[q.key()][c.DocID])
		}
		for m, v := range map[string][]float64{"bm25": bm, "candidate-oracle": oracle} {
			rows = append(rows, EvalRow{Year: q.Year, QueryKey: q.key(), Method: m, Metrics: metrics(q, v, rels[q.key()]), Success: true})
		}
		for _, m := range methods {
			var j Job
			if e = readJSON(filepath.Join(out, "jobs", m+"-"+q.key()+".json"), &j); e != nil {
				return nil, nil, nil, e
			}
			if j.Method != m || j.QueryKey != q.key() || len(j.Scores) != len(q.Candidates) || len(j.DocIDs) != len(q.Candidates) {
				return nil, nil, nil, fmt.Errorf("job identity mismatch")
			}
			for i, c := range q.Candidates {
				if j.DocIDs[i] != c.DocID || math.IsNaN(j.Scores[i]) || math.IsInf(j.Scores[i], 0) {
					return nil, nil, nil, fmt.Errorf("job score/id mismatch")
				}
			}
			jobs[m] = append(jobs[m], j)
			rows = append(rows, EvalRow{Year: q.Year, QueryKey: q.key(), Method: m, Metrics: metrics(q, j.Scores, rels[q.key()]), Seconds: j.Seconds, Success: j.Success, Reused: j.Reused})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Method != rows[j].Method {
			return rows[i].Method < rows[j].Method
		}
		return rows[i].QueryKey < rows[j].QueryKey
	})
	return qs, rows, jobs, nil
}
func selectRows(rows []EvalRow, m, y string) []EvalRow {
	a := []EvalRow{}
	for _, r := range rows {
		if r.Method == m && (y == "all" || r.Year == y) {
			a = append(a, r)
		}
	}
	return a
}
func aggregate(a []EvalRow) M {
	nd, mrr, rec, times, ties := []float64{}, []float64{}, []float64{}, []float64{}, []float64{}
	success := 0
	recall100, judged := []float64{}, []float64{}
	for _, r := range a {
		recall100 = append(recall100, r.Metrics.Recall100)
		judged = append(judged, r.Metrics.Judged)
		nd = append(nd, r.Metrics.NDCG)
		mrr = append(mrr, r.Metrics.MRR)
		rec = append(rec, r.Metrics.Recall10)
		ties = append(ties, r.Metrics.TieFraction)
		if r.Success {
			success++
		}
		if r.Reused == 0 {
			times = append(times, r.Seconds)
		}
	}
	return M{"queries": len(a), "successes": success, "ndcg_at_10": mean(nd), "mrr_at_10": mean(mrr), "recall_at_10": mean(rec), "recall_at_100": mean(recall100), "candidate_judged_fraction": mean(judged), "mean_tie_fraction": mean(ties), "query_p50_s": percentile(times, .5), "query_p95_s": percentile(times, .95), "fresh_timing_queries": len(times)}
}
func report(out string) error {
	qs, rows, jobs, e := evidence(out)
	if e != nil {
		return e
	}
	res, e := resources(out)
	if e != nil {
		return e
	}
	summary := M{"queries": len(qs), "candidate_depth": 100, "resources": res, "aggregates": M{}, "paired_vs_qwen": M{}, "bootstrap_unit": "query; stratified by year; 10000 replicates; seed 20260918"}
	summary["paired_vs_original_jev"] = M{}
	ag := summary["aggregates"].(M)
	pairs := summary["paired_vs_qwen"].(M)
	var b strings.Builder
	b.WriteString("---\ntitle: Jev versus Qwen TREC DL results\ntype: experiment-results\nauthor: Kyle Wild\n---\n\n# Jev versus Qwen: TREC DL rerank@100\n\nHuman graded relevance, published BM25 top-100 candidates, no answer generation or LLM judge. Scores below use full official qrels and linear graded gains, as in NIST trec_eval. Costs are list-price estimates from observed usage, not invoices; missing-usage attempts are reported separately.\n\n| Method | DL19 nDCG@10 | DL20 nDCG@10 | Combined nDCG@10 | MRR@10 | Cost USD | Query p50 / p95 s |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, m := range append([]string{"bm25", "candidate-oracle"}, methods...) {
		for _, y := range []string{"dl19", "dl20", "all"} {
			ag[m+"/"+y] = aggregate(selectRows(rows, m, y))
		}
		a := ag[m+"/all"].(M)
		a19 := ag[m+"/dl19"].(M)
		a20 := ag[m+"/dl20"].(M)
		cost := 0.0
		if res[m] != nil {
			cost = res[m].USD
		}
		v19, v20 := "—", "—"
		if a19["queries"].(int) > 0 {
			v19 = fmt.Sprintf("%.4f", a19["ndcg_at_10"])
		}
		if a20["queries"].(int) > 0 {
			v20 = fmt.Sprintf("%.4f", a20["ndcg_at_10"])
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.4f | %.4f | $%.6f | %.2f / %.2f |\n", m, v19, v20, a["ndcg_at_10"], a["mrr_at_10"], cost, a["query_p50_s"], a["query_p95_s"])
	}
	b.WriteString("\n## Paired differences versus Qwen\n\n| Jev method | nDCG@10 difference | Paired 95% bootstrap interval |\n|---|---:|---:|\n")
	for _, m := range methods[1:] {
		for _, y := range []string{"dl19", "dl20", "all"} {
			if len(selectRows(rows, m, y)) == 0 {
				continue
			}
			d, l, h := bootstrap(selectRows(rows, m, y), selectRows(rows, "qwen", y))
			pairs[m+"/"+y] = M{"difference": d, "lower": l, "upper": h}
			if y == "all" {
				fmt.Fprintf(&b, "| %s | %+.4f | [%+.4f, %+.4f] |\n", m, d, l, h)
			}
		}
	}
	if len(methods) > 4 {
		b.WriteString("\nExploratory prompt study: baseline results and timings were imported from the earlier collection. Nine variants were frozen before their new calls, after the initial benchmark had been observed. Selection of the best observed prompt is not held-out validation. See [prompt study protocol](../../PROMPT_STUDY.md).\n")
		for _, m := range methods {
			if _, ok := promptVariants[m]; ok {
				d, l, h := bootstrap(selectRows(rows, m, "all"), selectRows(rows, baseMethod(m), "all"))
				summary["paired_vs_original_jev"].(M)[m] = M{"difference": d, "lower": l, "upper": h}
			}
		}
	}
	b.WriteString("\nMethods and budget were frozen before benchmark inference. These intervals describe individual comparisons, not simultaneous familywise coverage or a non-inferiority test. Query wall times include client queuing within a query, HTTP, parsing and persistence; Qwen uses one 100-document request and Jev up to 16 concurrent single-passage requests. They describe these deployed implementations, not intrinsic model speed. Original retrieval and oracle have no inference latency.\n\nThe oracle uses labels and is only a candidate-set ceiling. All systems see the same 100 candidates; recall@100 is therefore unchanged by successful reranking. Unjudged passages count as zero gain in the standard metric but are not independently verified irrelevant. These English public benchmarks may overlap model training and contain only 97 queries. No broad domain-general or statistical equivalence claim follows.\n\n[Protocol](../../PROTOCOL-v1.md) · [Per-query metrics](per-query.json) · [Full summary and resource accounting](summary.json) · [Audit](audit.json)\n")
	fmt.Fprintf(&b, "\nEvaluated queries in this output: %d.\n", len(qs))
	if _, e := os.Stat(filepath.Join(out, "reanalysis.json")); e == nil {
		b.WriteString("\nThis is a separately versioned offline replay correcting floating-point tie handling. Original responses and timings are unchanged. [Correction and sensitivity](../../ANALYSIS_CORRECTION.md).\n")
	}
	if e = writeJSON(filepath.Join(out, "per-query.json"), rows); e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(out, "summary.json"), summary); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(out, "RESULTS.md"), []byte(b.String())); e != nil {
		return e
	}
	for _, m := range append([]string{"bm25"}, methods...) {
		for _, y := range []string{"dl19", "dl20"} {
			var run strings.Builder
			for qi, q := range qs {
				if q.Year != y {
					continue
				}
				v := []float64{}
				if m == "bm25" {
					for _, c := range q.Candidates {
						v = append(v, c.Score)
					}
				} else {
					v = jobs[m][qi].Scores
				}
				for rank, i := range order(v) {
					fmt.Fprintf(&run, "%d Q0 %s %d %d %s\n", q.Query.QID, q.Candidates[i].DocID, rank+1, len(v)-rank, m)
				}
			}
			if e = atomicWrite(filepath.Join(out, "runs", m+"-"+y+".trec"), []byte(run.String())); e != nil {
				return e
			}
		}
	}
	fmt.Print(b.String())
	return nil
}
func audit(out string) error {
	if e := verifyBaselineImport(out); e != nil {
		return e
	}
	qs, rows, jobs, e := evidence(out)
	if e != nil {
		return e
	}
	var manifest M
	if e = readJSON(filepath.Join(out, "manifest.json"), &manifest); e != nil {
		return e
	}
	h, e := sourceHash()
	if e != nil {
		return e
	}
	if manifest["source_sha256"] != h {
		return fmt.Errorf("source differs from frozen manifest")
	}
	res, e := resources(out)
	if e != nil {
		return e
	}
	expectedReceipts := map[string]bool{}
	replayed := 0
	for _, m := range methods {
		for qi, q := range qs {
			j := jobs[m][qi]
			if !j.Success {
				return fmt.Errorf("audit requires explicit review of failed job %s", j.ID)
			}
			n := len(q.Candidates)
			if m == "qwen" {
				n = 1
			}
			reconstructed := []float64{}
			for i := 0; i < n; i++ {
				id := j.ID
				cs := q.Candidates
				if m != "qwen" {
					id = fmt.Sprintf("%s-%03d", id, i)
					cs = q.Candidates[i : i+1]
				}
				p := payload(m, q, cs)
				found := false
				for attempt := 1; attempt <= 3; attempt++ {
					rid := id + "-" + strconv.Itoa(attempt)
					path := filepath.Join(out, "receipts", rid+".json")
					var r Receipt
					if e = readJSON(path, &r); os.IsNotExist(e) {
						break
					} else if e != nil {
						return e
					}
					expectedReceipts[rid] = true
					if r.PayloadSHA != digest(encode(p)) || r.ID != rid || r.Method != m {
						return fmt.Errorf("receipt replay identity mismatch %s", rid)
					}
					if r.Error != "" || r.Status < 200 || r.Status >= 300 {
						continue
					}
					var body M
					if e = json.Unmarshal(r.Response, &body); e != nil {
						return e
					}
					v, e := scores(m, body, len(cs))
					if e != nil {
						return e
					}
					reconstructed = append(reconstructed, v...)
					found = true
					replayed++
					break
				}
				if !found {
					return fmt.Errorf("missing successful receipt %s", id)
				}
			}
			for i, v := range reconstructed {
				if math.Abs(v-j.Scores[i]) > 1e-12 {
					return fmt.Errorf("score replay mismatch %s", j.ID)
				}
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(out, "receipts", "*.json"))
	if len(files) != len(expectedReceipts) {
		return fmt.Errorf("unexpected receipt files")
	}
	compared := 0
	for _, m := range append([]string{"bm25"}, methods...) {
		for _, y := range []string{"dl19", "dl20"} {
			if len(selectRows(rows, m, y)) == 0 {
				continue
			}
			for _, metric := range []string{"ndcg_cut.10", "recip_rank", "recall.10,100"} {
				args := []string{"-q", "-c", "-m", metric}
				if metric != "ndcg_cut.10" {
					args = append(args, "-l", "2")
				}
				if metric == "recip_rank" {
					args = append(args, "-M", "10")
				}
				args = append(args, "data/"+y+".qrels", filepath.Join(out, "runs", m+"-"+y+".trec"))
				raw, e := exec.Command("third_party/trec_eval/trec_eval", args...).CombinedOutput()
				if e != nil {
					return fmt.Errorf("trec_eval: %s %w", raw, e)
				}
				for _, line := range strings.Split(string(raw), "\n") {
					p := strings.Fields(line)
					if len(p) != 3 || p[1] == "all" {
						continue
					}
					v, e := strconv.ParseFloat(p[2], 64)
					if e != nil {
						return e
					}
					found := false
					for _, r := range rows {
						if r.Method != m || r.QueryKey != y+"-"+p[1] {
							continue
						}
						found = true
						expected := r.Metrics.NDCG
						switch p[0] {
						case "recip_rank":
							expected = r.Metrics.MRR
						case "recall_10":
							expected = r.Metrics.Recall10
						case "recall_100":
							expected = r.Metrics.Recall100
						}
						if math.Abs(v-expected) > .000051 {
							return fmt.Errorf("trec_eval disagreement %s %s %s: %f vs %f", m, r.QueryKey, p[0], v, expected)
						}
						compared++
					}
					if !found {
						continue // trec_eval -c also emits unselected queries for diagnostic subsets
					}
				}
			}
		}
	}
	if compared != len(qs)*(len(methods)+1)*4 {
		return fmt.Errorf("incomplete official metric comparisons: %d", compared)
	}
	result := M{"passed": true, "queries": len(qs), "jobs": len(qs) * len(methods), "replayed_successful_requests": replayed, "receipt_attempts": len(files), "trec_eval_per_query_metric_comparisons": compared, "source_sha256": h, "resources": res, "candidate_inputs_verified": true, "gold_labels_excluded_from_payloads": true}
	if e = writeJSON(filepath.Join(out, "audit.json"), result); e != nil {
		return e
	}
	fmt.Printf("Audit passed: %d queries, %d jobs, %d replayed requests, %d official metric comparisons.\n", len(qs), len(qs)*len(methods), replayed, compared)
	return nil
}
