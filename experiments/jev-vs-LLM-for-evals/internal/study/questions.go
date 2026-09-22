package study

import (
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"path/filepath"
)

func CampaignQuestionAudit(dir string) (out M, err error) {
	defer eval.Recover(&err)
	manifest := m(eval.ReadJSON(filepath.Join(dir, "campaign.json")))
	groups := map[string]map[string][]M{}
	for _, v := range a(manifest["shards"]) {
		spec := m(v)
		jobs := a(eval.ReadJSON(filepath.Join(dir, s(spec["directory"]), "jobs.json")))
		if eval.Hash(eval.Canon(jobs)) != spec["jobs_sha256"] {
			panic("question audit frozen job hash mismatch")
		}
		for _, v := range jobs {
			j := m(v)
			if j["model"] != "jev-latest" {
				continue
			}
			if !(j["benchmark"] == "llmbar" && j["method"] == "Vanilla") && !(j["benchmark"] == "judgebench" && j["method"] == "vanilla") {
				continue
			}
			row := m(j["row"])
			benchmark := s(j["benchmark"])
			if groups[benchmark] == nil {
				groups[benchmark] = map[string][]M{}
			}
			hash := eval.Hash([]byte(s(row["input"])))
			groups[benchmark][hash] = append(groups[benchmark][hash], M{"case_id": row["id"], "subset": row["subset"], "fold": Fold(benchmark, s(row["subset"]), s(row["id"]))})
		}
	}
	summaries := []M{}
	for _, benchmark := range orderedKeys(groups) {
		qs := groups[benchmark]
		cases, duplicateClusters, heldExcluded := 0, 0, 0
		overlap := []M{}
		for _, hash := range orderedKeys(qs) {
			rs := qs[hash]
			cases += len(rs)
			if len(rs) > 1 {
				duplicateClusters++
			}
			dev, held := false, 0
			for _, r := range rs {
				if r["fold"] == "development" {
					dev = true
				} else {
					held++
				}
			}
			if dev && held > 0 {
				heldExcluded += held
				overlap = append(overlap, M{"question_sha256": hash, "cases": rs})
			}
		}
		summaries = append(summaries, M{"benchmark": benchmark, "cases": cases, "unique_exact_questions": len(qs), "duplicate_question_clusters": duplicateClusters, "cross_fold_question_clusters": len(overlap), "held_out_cases_with_development_question": heldExcluded, "overlap": overlap})
	}
	return M{"scope": "complete planned input metadata only; no model outputs used; exact text equality does not detect paraphrases or training contamination", "source_manifest_sha256": eval.Hash(eval.Canon(manifest)), "benchmarks": summaries}, nil
}
func unseenQuestionPairs(held, dev []paired) []paired {
	seen := map[string]bool{}
	hash := func(u Unit) string {
		h := s(u.Rows[0]["question_sha256"])
		if h == "" {
			panic("missing question identity in cascade")
		}
		return h
	}
	for _, p := range dev {
		seen[hash(p.A)] = true
	}
	out := []paired{}
	for _, p := range held {
		if !seen[hash(p.A)] {
			out = append(out, p)
		}
	}
	return out
}
