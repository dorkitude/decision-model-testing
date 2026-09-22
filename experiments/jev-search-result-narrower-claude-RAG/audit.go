package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func auditEvidence(run, data, source string) (M, error) {
	tables := map[string]map[string]M{}
	for _, name := range []string{"answers", "judgments", "filters", "searches", "requests", "jev_context", "reservations"} {
		m, e := readKeyed(filepath.Join(run, name+".jsonl"), "id")
		if e != nil {
			return nil, e
		}
		tables[name] = m
	}
	qs, e := readKeyed(filepath.Join(data, "questions.jsonl"), "question_id")
	if e != nil {
		return nil, e
	}
	excluded, e := excludedIDs(filepath.Join(data, "excluded-questions.jsonl"))
	if e != nil {
		return nil, e
	}
	if len(excluded) != 100 {
		return nil, fmt.Errorf("wrong exclusion count")
	}
	for id := range qs {
		if excluded[id] {
			return nil, fmt.Errorf("overlapping question: %s", id)
		}
	}
	units, e := readKeyed(filepath.Join(data, "units.jsonl"), "id")
	if e != nil {
		return nil, e
	}
	manifest, e := oneRow(filepath.Join(run, "manifest.jsonl"))
	if e != nil {
		return nil, e
	}
	cfg := obj(manifest["config"])
	paths, e := filepath.Glob(filepath.Join(source, "*.go"))
	if e != nil {
		return nil, e
	}
	paths = append(paths, filepath.Join(source, "go.mod"), filepath.Join(source, "go.sum"))
	hashes := M{}
	for _, p := range paths {
		h, e := fileHash(p)
		if e != nil {
			return nil, e
		}
		hashes[filepath.Base(p)] = h
	}
	if hash(canon(hashes)) != str(manifest["source_sha256"]) {
		return nil, fmt.Errorf("frozen harness source hash mismatch")
	}
	for file, key := range map[string]string{"questions.jsonl": "questions_sha256", "units.jsonl": "corpus_sha256", "excluded-questions.jsonl": "excluded_questions_sha256"} {
		h, e := fileHash(filepath.Join(data, file))
		if e != nil {
			return nil, e
		}
		if h != str(manifest[key]) {
			return nil, fmt.Errorf("%s hash mismatch", file)
		}
	}
	answers, judgments, filters, searches, requests, guards := tables["answers"], tables["judgments"], tables["filters"], tables["searches"], tables["requests"], tables["jev_context"]
	if len(qs) != int(num(cfg["questions"])) || len(qs) == 0 || len(answers) != len(qs)*2 || len(judgments) != len(qs)*2 {
		return nil, fmt.Errorf("incomplete paired coverage")
	}
	if len(tables["reservations"]) != len(requests) {
		return nil, fmt.Errorf("unresolved request reservation")
	}
	successful := map[string]M{}
	failures := []M{}
	for id, r := range requests {
		reservation := tables["reservations"][id]
		if reservation == nil {
			return nil, fmt.Errorf("unreserved request %s", id)
		}
		identity := hash(canon(M{"url": r["url"], "payload": r["payload"]}))
		if identity != str(r["payload_sha256"]) || identity != str(reservation["payload_sha256"]) || r["key"] != reservation["key"] {
			return nil, fmt.Errorf("request identity mismatch: %s", id)
		}
		if r["phase"] == "judge-jev" {
			return nil, fmt.Errorf("unexpected Jev judge")
		}
		if r["phase"] == "answer" && num(r["status"]) == 200 {
			raw := obj(obj(r["response"])["claude_raw"])
			parsed, err := claudeResponse(raw, str(obj(r["payload"])["tool_choice"]), str(cfg["qa_model"]))
			if err != nil {
				return nil, err
			}
			if string(canon(parsed)) != string(canon(r["response"])) {
				return nil, fmt.Errorf("Claude normalization mismatch")
			}
			for _, ev := range arr(raw["assistant_events"]) {
				for _, block := range arr(obj(obj(ev)["message"])["content"]) {
					if obj(block)["type"] == "tool_use" && obj(block)["name"] != "StructuredOutput" {
						return nil, fmt.Errorf("unexpected built-in tool use")
					}
				}
			}
		}
		status := num(r["status"])

		if status >= 200 && status < 300 {
			key := str(r["key"])
			if successful[key] != nil {
				return nil, fmt.Errorf("repeated successful request %s", key)
			}
			successful[key] = r
		} else {
			failures = append(failures, M{"id": id, "key": r["key"], "phase": r["phase"], "status": r["status"]})
		}
	}
	for id, s := range searches {
		results := arr(s["results"])
		if len(results) != int(num(cfg["result_k"])) {
			return nil, fmt.Errorf("search result count %s", id)
		}
		seen := map[string]bool{}
		for _, v := range results {
			u := obj(v)
			uid := str(u["id"])
			if seen[uid] || !same(u, units[uid]) {
				return nil, fmt.Errorf("changed/duplicate retrieved chunk %s", uid)
			}
			seen[uid] = true
		}
	}
	for id, f := range filters {
		q, s := qs[str(f["question_id"])], searches[str(f["search_id"])]
		if q == nil || s == nil || f["original_question"] != q["question"] || f["query"] != s["query"] {
			return nil, fmt.Errorf("filter identity %s", id)
		}
		decisions := arr(f["decisions"])
		results := arr(s["results"])
		if len(decisions) != len(results) {
			return nil, fmt.Errorf("filter coverage %s", id)
		}
		kept := []any{}
		for i, v := range decisions {
			d := obj(v)
			if d["chunk_id"] != obj(results[i])["id"] {
				return nil, fmt.Errorf("filter order %s", id)
			}
			if d["keep"] == true {
				kept = append(kept, d["chunk_id"])
			}
		}
		if !same(kept, arr(f["kept_ids"])) {
			return nil, fmt.Errorf("filter retained IDs %s", id)
		}
		index := 0
		for page, key := range arr(f["page_request_keys"]) {
			req := successful[str(key)]
			if req == nil {
				return nil, fmt.Errorf("missing filter receipt %v", key)
			}
			p := obj(req["payload"])
			state := obj(p["state"])
			pageResults := arr(state["results"])
			response := obj(obj(req["response"])["answers"])
			if state["original_question"] != q["question"] || state["search_query"] != s["query"] || len(response) != len(pageResults) {
				return nil, fmt.Errorf("filter page state %s", id)
			}
			for i, v := range pageResults {
				u := obj(v)
				original := units[str(u["chunk_id"])]
				if index >= len(decisions) || original == nil || u["text"] != original["text"] || u["document_id"] != original["document_id"] {
					return nil, fmt.Errorf("filter altered source %s", id)
				}
				d := obj(decisions[index])
				expectedDecision, err := filterDecision(str(cfg["filter_mode"]), obj(response[fmt.Sprintf("r%d", i)]), num(cfg["filter_threshold"]))
				if err != nil {
					return nil, err
				}
				if d["chunk_id"] != u["chunk_id"] || d["keep"] != expectedDecision["keep"] || num(d["page"]) != float64(page) {
					return nil, fmt.Errorf("filter decision mismatch %s", id)
				}
				if str(cfg["filter_mode"]) == "categorical" {
					if d["category"] != expectedDecision["category"] {
						return nil, fmt.Errorf("category mismatch")
					}
				} else if d["probability_useful"] != expectedDecision["probability_useful"] {
					return nil, fmt.Errorf("probability mismatch")
				}

				index++
			}
		}
		if index != len(decisions) {
			return nil, fmt.Errorf("filter paging omitted results %s", id)
		}
	}
	for qid, q := range qs {
		for _, arm := range []string{"baseline", "jev_filtered"} {
			aid := qid + "/" + arm
			a := answers[aid]
			if a == nil || a["question_id"] != qid || a["arm"] != arm {
				return nil, fmt.Errorf("missing/mismatched answer %s", aid)
			}
			messages := arr(a["messages"])
			ids, tools := arr(a["search_ids"]), arr(a["tool_results"])
			if len(messages) < 2 || !same(messages[1], M{"role": "user", "content": q["question"]}) || len(ids) != len(tools) || len(ids) != int(num(a["search_calls"])) || len(ids) > int(num(cfg["max_searches"])) || strings.TrimSpace(str(a["answer"])) == "" {
				return nil, fmt.Errorf("answer state %s", aid)
			}
			for i, sid := range ids {
				tool := obj(tools[i])
				s := searches[str(sid)]
				if s == nil {
					return nil, fmt.Errorf("missing search %v", sid)
				}
				expected := arr(s["results"])
				if arm == "baseline" {
					if tool["filter_id"] != "" {
						return nil, fmt.Errorf("baseline filtered %s", aid)
					}
				} else {
					fid := qid + "/" + str(sid)
					f := filters[fid]
					if f == nil || tool["filter_id"] != fid {
						return nil, fmt.Errorf("missing treatment filter %s", aid)
					}
					expected = []any{}
					for _, uid := range arr(f["kept_ids"]) {
						expected = append(expected, units[str(uid)])
					}
				}
				if !same(arr(tool["returned_results"]), expected) {
					return nil, fmt.Errorf("tool contents mismatch %s", aid)
				}
			}
			finalRequest := successful[str(a["final_request_key"])]
			if !same(obj(finalRequest["payload"])["transcript"], a["messages"]) {
				return nil, fmt.Errorf("answer transcript mismatch")
			}
			final := arr(obj(successful[str(a["final_request_key"])]["response"])["choices"])
			if len(final) == 0 || obj(final[0])["finish_reason"] != "stop" || strings.TrimSpace(str(obj(obj(final[0])["message"])["content"])) != a["answer"] {
				return nil, fmt.Errorf("answer receipt mismatch %s", aid)
			}
			for _, judge := range []string{"deepseek"} {
				j := judgments[aid+"/"+judge]
				correct, ok := j["correct"].(bool)
				if !ok {
					return nil, fmt.Errorf("missing/invalid judgment %s/%s", aid, judge)
				}
				req := successful[str(j["request_key"])]
				if req == nil {
					return nil, fmt.Errorf("missing judge receipt")
				}
				var state M
				var verdict bool
				{
					ms := arr(obj(req["payload"])["messages"])
					choices := arr(obj(req["response"])["choices"])
					if len(ms) < 2 || len(choices) == 0 {
						return nil, fmt.Errorf("invalid DeepSeek receipt")
					}
					if e = json.Unmarshal([]byte(str(obj(ms[1])["content"])), &state); e != nil {
						return nil, e
					}
					var result M
					if e = json.Unmarshal([]byte(str(obj(obj(choices[0])["message"])["content"])), &result); e != nil {
						return nil, e
					}
					verdict, ok = result["correct"].(bool)
					if !ok {
						return nil, fmt.Errorf("invalid DeepSeek verdict")
					}
				}
				expected := M{"question": q["question"], "submitted_answer": a["answer"], "reference_answer": q["answer"], "source": q["source"]}
				if !same(state, expected) || correct != verdict {
					return nil, fmt.Errorf("judge state/verdict mismatch %s/%s", aid, judge)
				}
			}
		}
	}
	maxTokens := float64(0)
	for _, req := range requests {
		if req["phase"] != "filter" {
			continue
		}
		g := guards[str(req["key"])]
		if g == nil || num(g["serialized_utf8_bytes"]) > num(cfg["jev_page_bytes"]) || num(g["cl100k_proxy_tokens"]) > num(cfg["jev_page_proxy_tokens"]) || num(g["question_count"]) > num(cfg["jev_max_questions"]) {
			return nil, fmt.Errorf("Jev guard failed %v", req["key"])
		}
		payload := req["payload"]
		if num(g["serialized_utf8_bytes"]) != float64(len(canon(payload))) || num(g["question_count"]) != float64(len(obj(obj(payload)["questions"]))) || g["payload_sha256"] != hash(canon(payload)) {
			return nil, fmt.Errorf("Jev guard identity mismatch %v", req["key"])
		}
		if num(req["status"]) == 200 {
			maxTokens = max(maxTokens, num(obj(obj(req["response"])["usage"])["input_tokens"]))
		}
	}
	summary, e := oneRow(filepath.Join(run, "summary.jsonl"))
	if e != nil {
		return nil, e
	}
	if summary["complete"] != true || num(summary["answers"]) != float64(len(answers)) || num(summary["judgments"]) != float64(len(judgments)) {
		return nil, fmt.Errorf("summary incomplete")
	}
	sort.Slice(failures, func(i, j int) bool { return str(failures[i]["id"]) < str(failures[j]["id"]) })
	return M{"id": "offline-audit", "passed": true, "questions": len(qs), "answers": len(answers), "judgments": len(judgments), "searches": len(searches), "filter_sets": len(filters), "max_jev_provider_reported_input_tokens": maxTokens, "failed_attempts": failures, "checks": []string{"frozen harness, sample and corpus hashes", "complete paired question IDs", "request identities and reservations", "unchanged retrieved text and IDs", "exact filtering coverage, probabilities, threshold and order", "agent tool contents match assigned arm", "identical treatment-blind judge states", "verdicts match provider receipts", "Jev request size guards"}}, nil
}
