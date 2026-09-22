package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func excludedIDs(path string) (map[string]bool, error) {
	ids := map[string]bool{}
	e := scan(path, func(b []byte) error {
		var q Question
		if e := json.Unmarshal(b, &q); e != nil {
			return e
		}
		if q.ID == "" {
			return fmt.Errorf("empty excluded ID")
		}
		ids[q.ID] = true
		return nil
	})
	return ids, e
}
func selectSample(rows []M, excluded map[string]bool, n, seed int) ([]M, error) {
	eligible := []M{}
	seen := map[string]bool{}
	for _, r := range rows {
		id := str(r["question_id"])
		if id == "" || seen[id] {
			return nil, fmt.Errorf("missing/duplicate question ID")
		}
		seen[id] = true
		if !excluded[id] {
			eligible = append(eligible, r)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		return hash([]byte(fmt.Sprintf("%d/%s", seed, str(eligible[i]["question_id"])))) < hash([]byte(fmt.Sprintf("%d/%s", seed, str(eligible[j]["question_id"]))))
	})
	if len(eligible) < n {
		return nil, fmt.Errorf("insufficient disjoint questions")
	}
	return eligible[:n], nil
}
func (h *Harness) prepareSample() error {
	excluded, e := excludedIDs(h.C.ExcludedQuestions)
	if e != nil {
		return e
	}
	if len(excluded) != 100 {
		return fmt.Errorf("expected 100 original question exclusions")
	}
	if _, e = os.Stat("data/questions.jsonl"); e == nil {
		qs, e := h.questions()
		if e != nil {
			return e
		}
		for _, q := range qs {
			if excluded[q.ID] {
				return fmt.Errorf("question overlaps original run: %s", q.ID)
			}
		}
		return nil
	}
	b, e := h.cli("questions", "export", "--set", "test", "--include-answer", "--include-source", "--data-dir", h.C.DataDir)
	if e != nil {
		return e
	}
	rows := []M{}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r M
		if e = json.Unmarshal(line, &r); e != nil {
			return e
		}
		rows = append(rows, r)
	}
	selected, e := selectSample(rows, excluded, h.C.Questions, h.C.Seed)
	if e != nil {
		return e
	}
	var out bytes.Buffer
	for _, r := range selected {
		out.Write(canon(r))
		out.WriteByte('\n')
	}
	if e = os.WriteFile("data/questions.jsonl", out.Bytes(), 0600); e != nil {
		return e
	}
	original, e := os.ReadFile(h.C.ExcludedQuestions)
	if e != nil {
		return e
	}
	return jsonFile("data/sample.json", M{"method": "ascending sha256(seed/question_id) after excluding original 100 IDs", "seed": h.C.Seed, "excluded_count": len(excluded), "excluded_sha256": hash(original), "eligible_count": len(rows) - len(excluded), "selected": len(selected), "questions_sha256": hash(out.Bytes()), "dataset_revision": revision})
}
