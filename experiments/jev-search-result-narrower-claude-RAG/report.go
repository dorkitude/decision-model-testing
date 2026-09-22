package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func usage(r M) (float64, float64) {
	u := obj(obj(r["response"])["usage"])
	in := num(u["prompt_tokens"])
	out := num(u["completion_tokens"])
	if _, ok := u["input_tokens"]; ok {
		in = num(u["input_tokens"])
	}
	if _, ok := u["output_tokens"]; ok {
		out = num(u["output_tokens"])
	}
	return in, out
}
func summarizeRequests(rs []M) M {
	var in, out, seconds float64
	unknown := 0
	for _, r := range rs {
		a, b := usage(r)
		in += a
		out += b
		seconds += num(r["elapsed_s"])
		if obj(r["response"])["usage"] == nil {
			unknown++
		}
	}
	return M{"requests": len(rs), "input_tokens": in, "output_tokens": out, "summed_api_seconds": seconds, "requests_without_usage": unknown}
}
func interval(values []float64) (float64, float64) {
	sort.Float64s(values)
	return values[int(.025*float64(len(values)))], values[int(.975*float64(len(values)))]
}
func pairedBootstrap(qs []Question, delta map[string]float64) (float64, float64) {
	groups := map[string][]string{}
	for _, q := range qs {
		groups[q.DocID] = append(groups[q.DocID], q.ID)
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rng := rand.New(rand.NewSource(20260918))
	values := make([]float64, 5000)
	for i := range values {
		sum := 0.
		n := 0
		for range keys {
			g := groups[keys[rng.Intn(len(keys))]]
			for _, qid := range g {
				sum += delta[qid]
				n++
			}
		}
		values[i] = 100 * sum / float64(n)
	}
	return interval(values)
}
func (h *Harness) report() error {
	if h.Units == nil {
		if e := h.loadUnits(); e != nil {
			return e
		}
	}
	qs, e := h.questions()
	if e != nil {
		return e
	}
	answers := h.S.all("answers")
	judgments := h.S.all("judgments")
	if len(answers) != 2*len(qs) || len(judgments) != 2*len(qs) {
		return fmt.Errorf("incomplete results: %d answers / %d judgments", len(answers), len(judgments))
	}
	amap := map[string]M{}
	for _, a := range answers {
		amap[str(a["id"])] = a
	}
	jmap := map[string]M{}
	for _, j := range judgments {
		jmap[str(j["id"])] = j
	}
	rows := []M{}
	comparisons := []M{}
	agreements := M{}
	for _, judge := range []string{"deepseek"} {
		correct := map[string]int{}
		delta := map[string]float64{}
		wins, losses := 0, 0
		for _, q := range qs {
			b, ok := jmap[q.ID+"/baseline/"+judge]
			if !ok {
				return fmt.Errorf("missing judgment")
			}
			f, ok := jmap[q.ID+"/jev_filtered/"+judge]
			if !ok {
				return fmt.Errorf("missing judgment")
			}
			bv, bok := b["correct"].(bool)
			fv, fok := f["correct"].(bool)
			if !bok || !fok {
				return fmt.Errorf("nonboolean verdict")
			}
			if bv {
				correct["baseline"]++
			}
			if fv {
				correct["jev_filtered"]++
			}
			if fv && !bv {
				wins++
				delta[q.ID] = 1
			}
			if bv && !fv {
				losses++
				delta[q.ID] = -1
			}
		}
		for _, arm := range []string{"baseline", "jev_filtered"} {
			rows = append(rows, M{"judge": judge, "arm": arm, "n": len(qs), "correct": correct[arm], "accuracy": float64(correct[arm]) / float64(len(qs))})
		}
		lo, hi := pairedBootstrap(qs, delta)
		comparisons = append(comparisons, M{"judge": judge, "filtered_minus_baseline_pp": 100 * float64(wins-losses) / float64(len(qs)), "filtered_only_correct": wins, "baseline_only_correct": losses, "cluster_bootstrap_95_percent_pp": []float64{lo, hi}, "cluster": "source_document_id", "resamples": 5000})
	}
	requests := h.S.all("requests")
	resources := M{}
	phaseGroups := map[string][]M{}
	for _, r := range requests {
		phase := str(r["phase"])
		phaseGroups[phase] = append(phaseGroups[phase], r)
		if hash(canon(M{"url": r["url"], "payload": r["payload"]})) != str(r["payload_sha256"]) {
			return fmt.Errorf("request payload digest mismatch")
		}
	}
	for _, arm := range []string{"baseline", "jev_filtered"} {
		answerRequests, filterRequests, searchRequests := []M{}, []M{}, []M{}
		searchSet, filterSet := map[string]bool{}, map[string]bool{}
		calls, chunkCount, contextTokens := 0., 0., 0.
		for _, q := range qs {
			a, ok := amap[q.ID+"/"+arm]
			if !ok || str(a["answer"]) == "" {
				return fmt.Errorf("missing answer")
			}
			calls += num(a["search_calls"])
			chunkCount += num(a["returned_chunk_count"])
			contextTokens += num(a["returned_context_proxy_tokens"])
			for _, v := range arr(a["search_ids"]) {
				searchSet[str(v)] = true
			}
			for _, v := range arr(a["filter_ids"]) {
				filterSet[str(v)] = true
			}
		}
		for _, r := range requests {
			key := str(r["key"])
			if strings.HasPrefix(key, "answer/") && strings.Contains(key, "/"+arm+"/") {
				answerRequests = append(answerRequests, r)
			}
			if strings.HasPrefix(key, "search/") {
				parts := strings.Split(key, "/")
				if len(parts) > 1 && searchSet[parts[1]] {
					searchRequests = append(searchRequests, r)
				}
			}
			if strings.HasPrefix(key, "filter/") {
				parts := strings.Split(key, "/")
				if len(parts) > 3 && filterSet[parts[1]+"/"+parts[2]] {
					filterRequests = append(filterRequests, r)
				}
			}
		}
		all := append(append(append([]M{}, answerRequests...), filterRequests...), searchRequests...)
		resources[arm] = M{"qa": summarizeRequests(answerRequests), "filter": summarizeRequests(filterRequests), "search": summarizeRequests(searchRequests), "total": summarizeRequests(all), "search_calls": calls, "returned_chunks": chunkCount, "returned_context_proxy_tokens": contextTokens, "mean_search_calls": calls / float64(len(qs))}
	}
	filters := h.S.all("filters")
	total, before, after, pages := 0, 0, 0, 0
	maxProxy, maxBytes := 0, 0
	for _, f := range filters {
		ds := arr(f["decisions"])
		kept := arr(f["kept_ids"])
		seen := map[string]bool{}
		ids := []string{}
		for _, v := range ds {
			d := obj(v)
			id := str(d["chunk_id"])
			if seen[id] {
				return fmt.Errorf("duplicate filter decision")
			}
			seen[id] = true
			if b, _ := d["keep"].(bool); b {
				ids = append(ids, id)
			}
		}
		if string(canon(ids)) != string(canon(kept)) {
			return fmt.Errorf("filter retention/order mismatch")
		}
		s, ok := h.S.get("searches", str(f["search_id"]))
		if !ok || len(ds) != len(arr(s["results"])) {
			return fmt.Errorf("filter omitted search result")
		}
		for i, d := range ds {
			if str(obj(d)["chunk_id"]) != str(obj(arr(s["results"])[i])["id"]) {
				return fmt.Errorf("filter input membership/order mismatch")
			}
		}
		total += len(ds)
		pages += int(num(f["pages"]))
	}
	// Source-path coverage is a coarse diagnostic, not complete relevance ground truth.
	for _, q := range qs {
		a := amap[q.ID+"/baseline"]
		s, _ := h.S.get("searches", str(arr(a["search_ids"])[0]))
		f, ok := h.S.get("filters", q.ID+"/"+str(s["id"]))
		if !ok {
			return fmt.Errorf("missing initial filter")
		}
		hit := false
		for _, v := range arr(s["results"]) {
			if str(obj(v)["document_id"]) == q.DocID {
				hit = true
			}
		}
		if hit {
			before++
		}
		hit = false
		for _, v := range arr(f["kept_ids"]) {
			u := h.Units[str(v)]
			if u.DocID == q.DocID {
				hit = true
			}
		}
		if hit {
			after++
		}
	}
	keptTotal := 0
	for _, f := range filters {
		keptTotal += len(arr(f["kept_ids"]))
	}
	for _, r := range h.S.all("jev_context") {
		p := int(num(r["cl100k_proxy_tokens"]))
		b := int(num(r["serialized_utf8_bytes"]))
		if p > h.C.JevPageTokens || b > h.C.JevPageBytes {
			return fmt.Errorf("Jev context guard violated")
		}
		maxProxy = max(maxProxy, p)
		maxBytes = max(maxBytes, b)
	}
	phaseSummary := M{}
	for phase, rs := range phaseGroups {
		phaseSummary[phase] = summarizeRequests(rs)
	}
	summary := M{"id": "summary", "questions": len(qs), "answers": len(answers), "judgments": len(judgments), "scores": rows, "paired_comparisons": comparisons, "judge_agreement": agreements, "resources": resources, "actual_request_totals_by_phase": phaseSummary, "filtering": M{"calls": len(filters), "pages": pages, "results_scored": total, "results_kept": keptTotal, "retained_fraction": float64(keptTotal) / float64(total), "initial_gold_source_hit_baseline": before, "initial_gold_source_hit_filtered": after}, "jev_context": M{"max_proxy_tokens": maxProxy, "max_serialized_bytes": maxBytes, "guard_passed": true}, "complete": true}
	// Derived reports can be regenerated; primary JSONL evidence is append-only.
	if e = os.WriteFile(filepath.Join(h.S.Dir, "summary.jsonl"), append(canon(summary), '\n'), 0600); e != nil {
		return e
	}
	var b strings.Builder
	b.WriteString("---\ntitle: Claude RAG results\ntype: results\n---\n\n## Results\n\nCompleted 100 paired test questions, 200 Claude Opus 5 answers, and 200 DeepSeek judge verdicts (one per answer). No failed or missing cases are excluded.\n\n| Judge | Hybrid + rerank | + Jev filtering | Difference | Paired 95% interval |\n|---|---:|---:|---:|---:|\n")
	for _, judge := range []string{"deepseek"} {
		counts := map[string]int{}
		for _, q := range qs {
			for _, arm := range []string{"baseline", "jev_filtered"} {
				if jmap[q.ID+"/"+arm+"/"+judge]["correct"] == true {
					counts[arm]++
				}
			}
		}
		var comp M
		for _, c := range comparisons {
			if c["judge"] == judge {
				comp = c
			}
		}
		ci := comp["cluster_bootstrap_95_percent_pp"].([]float64)
		fmt.Fprintf(&b, "| %s | %d/%d (%.1f%%) | %d/%d (%.1f%%) | %+.1f pp | [%+.1f, %+.1f] pp |\n", judge, counts["baseline"], len(qs), 100*float64(counts["baseline"])/float64(len(qs)), counts["jev_filtered"], len(qs), 100*float64(counts["jev_filtered"])/float64(len(qs)), comp["filtered_minus_baseline_pp"], ci[0], ci[1])
	}
	fmt.Fprintf(&b, "\nJev kept **%d/%d retrieved chunks (%.1f%%)** across %d search-result sets, using **%d context-bounded filter pages**. Initial-search source-document coverage was **%d/100 before filtering and %d/100 afterward**; source-path matching is an incomplete evidence-recall proxy.\n\n", keptTotal, total, 100*float64(keptTotal)/float64(total), len(filters), pages, before, after)
	b.WriteString("| Answering pipeline resource | Baseline | Jev filtered |\n|---|---:|---:|\n")
	for _, metric := range []string{"input_tokens", "output_tokens", "summed_api_seconds"} {
		base := obj(obj(resources["baseline"])["total"])
		filtered := obj(obj(resources["jev_filtered"])["total"])
		fmt.Fprintf(&b, "| %s | %.2f | %.2f |\n", metric, num(base[metric]), num(filtered[metric]))
	}
	fmt.Fprintf(&b, "\nMaximum Jev request size: **%d cl100k_base proxy tokens / %d serialized UTF-8 bytes**, below the configured 6,000-token proxy and 24,000-byte guards. Every retrieved result has exactly one retained filtering decision.\n", maxProxy, maxBytes)
	b.WriteString("\nResource totals include QA, search embedding/reranking, and filtering, and exclude answer evaluation and index setup. Shared cached searches are charged once per arm that used them; the raw ledger records actual calls separately. Summed API seconds describe serial service work, not wall-clock throughput or a deployed latency guarantee. Turbopuffer requests do not expose LLM tokens. No dollar estimate or invoice reconciliation is claimed.\n\nIntervals are descriptive paired bootstrap intervals clustered by source document (5,000 resamples), without multiplicity adjustment. These are DeepSeek's correctness assessments, not independently adjudicated human accuracy. Jev only filters; DeepSeek is the sole judge. Questions were sampled once before scored inference; no threshold tuning on test outcomes was performed.\n")
	return os.WriteFile(filepath.Join(h.S.Dir, "results-section.md"), []byte(b.String()), 0600)
}

// Decode helper used by integrity tests and external artifact readers.
func decodeRecord(b []byte) (M, error) { var r M; e := json.Unmarshal(b, &r); return r, e }
