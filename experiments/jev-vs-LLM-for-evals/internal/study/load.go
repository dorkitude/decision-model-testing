// Package study reduces archived campaign receipts into paired judgment records.
// It reads completed shards only; active append-only files are never analyzed.
package study

import (
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
)

type M = map[string]any

func m(v any) M {
	if v == nil {
		return M{}
	}
	return v.(map[string]any)
}
func a(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}
func s(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func n(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	}
	return 0
}
func b(v any) bool { x, _ := v.(bool); return x }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func plus(r M, key string, v float64) { r[key] = n(r[key]) + v }
func Fold(benchmark, subset, id string) string {
	if subset == "Ties" {
		_, id, _ = strings.Cut(id, ":")
	}
	raw, e := hex.DecodeString(eval.Hash(eval.Canon([]any{"cascade-development-v1", benchmark, subset, id}))[:8])
	must(e)
	value := uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	if value%5 == 0 {
		return "development"
	}
	return "held_out"
}
func LoadCampaign(dir string) (records []M, manifest M, err error) {
	defer eval.Recover(&err)
	return loadManifest(dir, m(eval.ReadJSON(filepath.Join(dir, "campaign.json"))))
}
func loadManifest(dir string, input M) (records []M, manifest M, err error) {
	defer eval.Recover(&err)
	manifest = input
	pricing := m(manifest["pricing"])
	for _, v := range a(manifest["shards"]) {
		spec := m(v)
		shard := filepath.Join(dir, s(spec["directory"]))
		statusPath := filepath.Join(shard, "status-go.json")
		if _, e := os.Stat(statusPath); os.IsNotExist(e) {
			if _, e := os.Stat(statusPath + ".gz"); os.IsNotExist(e) {
				continue
			}
		}
		status := m(eval.ReadJSON(statusPath))
		if !b(status["complete"]) {
			continue
		}
		jobs := a(eval.ReadJSON(filepath.Join(shard, "jobs.json")))
		if eval.Hash(eval.Canon(jobs)) != spec["jobs_sha256"] {
			panic("shard input hash mismatch")
		}
		byKey := map[string]M{}
		byRequest := map[string]struct {
			key   string
			order int
		}{}
		eval.ForEachLine(filepath.Join(shard, "results.jsonl"), func(result M) {
			key := s(result["key"])
			if byKey[key] != nil {
				panic("duplicate job result")
			}
			r := M{}
			for _, field := range []string{"key", "benchmark", "method", "model", "case_id", "subset", "split", "valid", "published_score", "metrics", "scores", "num_correct", "decisions", "choice", "position"} {
				if value, ok := result[field]; ok {
					r[field] = value
				}
			}
			r["source_shard"] = spec["directory"]
			r["input_tokens"], r["output_tokens"], r["cached_input_tokens"], r["reasoning_tokens"] = 0.0, 0.0, 0.0, 0.0
			r["known_input_usd"], r["known_output_usd"], r["known_total_usd"], r["accounting_usd"] = 0.0, 0.0, 0.0, 0.0
			r["unknown_cost_attempts"], r["request_attempts"], r["orphan_reservations"] = 0, 0, 0
			r["transport_failures"], r["truncated_replies"], r["helper_attempts"], r["helper_accounting_usd"] = 0, 0, 0, 0.0
			r["request_seconds"], r["native_wall_seconds"] = 0.0, 0.0
			r["request_latencies"] = []any{}
			r["choice_confidence"] = []any{nil, nil}
			r["choice_p_original_a"] = []any{nil, nil}
			r["resolved_models"] = M{}
			r["error_attempts"] = M{}
			r["terminal_service_failures"] = 0
			if r["method"] == "Atomic" {
				r["native_orders"] = []any{M{}, M{}}
			}
			if r["scores"] != nil {
				expected := make([]any, len(a(r["scores"])))
				r["expected_scores"] = expected
			}
			byKey[key] = r
			// Select the final relevant typed decision stage for each presented order.
			for _, x := range a(result["stages"]) {
				stage := m(x)
				request := s(stage["request_key"])
				order := -1
				name := s(stage["stage"])
				if name == "initial-0" || name == "synthesis-0" || strings.HasSuffix(request, "/order-0") {
					order = 0
				}
				if name == "initial-1" || name == "synthesis-1" || strings.HasSuffix(request, "/order-1") {
					order = 1
				}
				if order >= 0 {
					if r["method"] == "Atomic" {
						a(r["native_orders"])[order] = M{"presentation_order": order, "candidate_orientation": "A/B as presented; reversed in order 1", "answers": stage["answers"], "criterion_scores": stage["criterion_scores"], "decision": stage["decision"], "valid": stage["valid"], "request_key": request}
					}
					for old, ref := range byRequest {
						if ref.key == key && ref.order == order {
							delete(byRequest, old)
						}
					}
					byRequest[request] = struct {
						key   string
						order int
					}{key, order}
				}
			}
		})
		if len(byKey) != len(jobs) {
			panic("completed shard has missing results")
		}
		for _, v := range jobs {
			job := m(v)
			r := byKey[s(job["key"])]
			if r == nil {
				panic("unexpected job identity")
			}
			row := m(job["row"])
			r["label"] = row["label"]
			r["comparison_content_sha256"] = eval.Hash(eval.Canon(row))
			text := row["input"]
			if text == nil {
				text = row["prompt"]
			}
			r["question_sha256"] = eval.Hash([]byte(s(text)))
			r["fold"] = Fold(s(r["benchmark"]), s(r["subset"]), s(r["case_id"]))
		}
		receiptIDs := map[string]bool{}
		eval.ForEachLine(filepath.Join(shard, "requests.jsonl"), func(receipt M) {
			key, _, _ := strings.Cut(s(receipt["key"]), "/")
			r := byKey[key]
			if r == nil {
				panic("orphan receipt")
			}
			id := s(receipt["attempt_id"])
			if id != "" {
				if receiptIDs[id] {
					panic("duplicate receipt attempt")
				}
				receiptIDs[id] = true
			}
			priced := eval.PriceReceipt(receipt, pricing)
			plus(r, "request_attempts", 1)
			plus(r, "input_tokens", n(priced["input_tokens"]))
			plus(r, "output_tokens", n(priced["output_tokens"]))
			plus(r, "cached_input_tokens", n(priced["cached_input_tokens"]))
			plus(r, "reasoning_tokens", n(priced["reasoning_tokens"]))
			plus(r, "known_input_usd", n(priced["estimated_input_usd"]))
			plus(r, "known_output_usd", n(priced["estimated_output_usd"]))
			plus(r, "known_total_usd", n(priced["estimated_total_usd"]))
			cost := priced["estimated_total_usd"]
			if cost == nil {
				plus(r, "unknown_cost_attempts", 1)
				cost = receipt["reserved_usd"]
			}
			plus(r, "accounting_usd", n(cost))
			plus(r, "request_seconds", n(receipt["latency_s"]))
			r["request_latencies"] = append(a(r["request_latencies"]), receipt["latency_s"])
			if !b(receipt["transport_ok"]) {
				plus(r, "transport_failures", 1)
				category := s(receipt["error"])
				if category == "" {
					category = "unspecified_transport_failure"
				}
				plus(m(r["error_attempts"]), category, 1)
				if b(receipt["terminal"]) && RetryableServiceError(category) {
					plus(r, "terminal_service_failures", 1)
				}
			}
			if b(priced["truncated"]) {
				plus(r, "truncated_replies", 1)
			}
			if receipt["model"] != r["model"] {
				plus(r, "helper_attempts", 1)
				plus(r, "helper_accounting_usd", n(cost))
			}
			resolved := s(m(receipt["response"])["model"])
			if resolved != "" {
				m(r["resolved_models"])[resolved] = true
			}
			if !b(receipt["terminal"]) {
				return
			}
			if r["method"] == "Atomic" && strings.HasPrefix(s(receipt["model"]), "jev") {
				if ref, ok := byRequest[s(receipt["key"])]; ok {
					probs := M{}
					for key, value := range m(m(receipt["response"])["answers"]) {
						probs[key] = m(value)["noul"]
					}
					m(a(r["native_orders"])[ref.order])["noul_probabilities"] = probs
				}
			}
			answer := m(m(m(receipt["response"])["answers"])["evaluation"])
			if ref, ok := byRequest[s(receipt["key"])]; ok && strings.HasPrefix(s(receipt["model"]), "jev") {
				probs := m(answer["probabilities"])
				sum := 0.0
				valid := len(probs) > 0
				for _, key := range sortedProbabilityKeys(probs) {
					p, ok := probs[key].(float64)
					if !ok || math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 1 {
						valid = false
					}
					sum += p
				}
				if valid && sum > 0 {
					pa := (n(probs["A"]) + n(probs["Output (a)"]) + n(probs["A>B"]) + n(probs["A>>B"])) / sum
					pb := (n(probs["B"]) + n(probs["Output (b)"]) + n(probs["B>A"]) + n(probs["B>>A"])) / sum
					if ref.order == 1 {
						pa, pb = pb, pa
					}
					a(r["choice_p_original_a"])[ref.order] = pa
					ds := a(r["decisions"])
					if len(ds) == 2 {
						confidence := n(probs["A=B"]) / sum
						if ds[ref.order] == "1" {
							confidence = pa
						} else if ds[ref.order] == "2" {
							confidence = pb
						}
						a(r["choice_confidence"])[ref.order] = confidence
					}
				}
			}
			if strings.HasPrefix(s(receipt["model"]), "jev") && r["expected_scores"] != nil {
				_, index, ok := strings.Cut(s(receipt["key"]), "/rating-")
				if ok {
					idx, e := strconv.Atoi(index)
					if e == nil {
						offset := 0.0
						if r["benchmark"] == "llmbar" {
							idx--
						} else {
							offset = 1
						}
						if value, ok := answer["score"].(float64); ok && idx >= 0 && idx < len(a(r["expected_scores"])) {
							a(r["expected_scores"])[idx] = value + offset
						}
					}
				}
			}
		})
		eval.ForEachLine(filepath.Join(shard, "reservations.jsonl"), func(reservation M) {
			if receiptIDs[s(reservation["attempt_id"])] {
				return
			}
			key, _, _ := strings.Cut(s(reservation["key"]), "/")
			r := byKey[key]
			if r == nil {
				panic("orphan reservation")
			}
			plus(r, "orphan_reservations", 1)
			plus(r, "unknown_cost_attempts", 1)
			plus(r, "accounting_usd", n(reservation["reserved_usd"]))
			if reservation["model"] != r["model"] {
				plus(r, "helper_accounting_usd", n(reservation["reserved_usd"]))
			}
		})
		eval.ForEachLine(filepath.Join(shard, "job-timings.jsonl"), func(timing M) {
			r := byKey[s(timing["key"])]
			if r == nil {
				panic("orphan timing")
			}
			plus(r, "native_wall_seconds", n(timing["active_wall_s"]))
		})
		for _, v := range jobs {
			r := byKey[s(m(v)["key"])]
			r["estimated_total_usd"] = nil
			if n(r["unknown_cost_attempts"]) == 0 {
				r["estimated_total_usd"] = r["known_total_usd"]
			}
			records = append(records, r)
		}
	}
	return
}
func WriteRecords(path string, rows []M) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	fields := []string{"key", "benchmark", "method", "model", "case_id", "subset", "fold", "valid", "published_score", "label", "decisions", "native_orders", "scores", "expected_scores", "choice", "choice_confidence", "choice_p_original_a", "question_sha256", "comparison_content_sha256", "source_shard", "original_source_shard", "service_retry_applied", "resolved_models", "error_attempts", "terminal_service_failures", "input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens", "known_input_usd", "known_output_usd", "estimated_total_usd", "known_total_usd", "accounting_usd", "unknown_cost_attempts", "request_attempts", "transport_failures", "truncated_replies", "helper_attempts", "helper_accounting_usd", "native_wall_seconds", "request_seconds"}
	if e = w.Write(fields); e != nil {
		return e
	}
	for _, r := range rows {
		line := make([]string, len(fields))
		for i, k := range fields {
			switch r[k].(type) {
			case []any, map[string]any:
				line[i] = string(eval.Canon(r[k]))
			default:
				line[i] = s(r[k])
			}
		}
		if e = w.Write(line); e != nil {
			return e
		}
	}
	w.Flush()
	return w.Error()
}

// Stable addition order makes probability normalization reproducible across processes.
func sortedProbabilityKeys(probs M) []string {
	keys := make([]string, 0, len(probs))
	for key := range probs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
