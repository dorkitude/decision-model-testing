package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func jevJudgePayload(model string, p M) M {
	return M{"model": model, "state": M{"question": p["question"], "submitted_answer": p["submitted_answer"], "reference_answer": p["reference_answer"], "source": p["source"]}, "questions": M{"evaluation": M{"type": "choice", "instructions": judgePrompt, "criteria": M{"correct": "The submitted answer is correct under the rubric.", "incorrect": "The submitted answer is incorrect under the rubric."}}}}
}
func rescoreInputs(run string) (map[string]M, map[string]M, M, error) {
	packets, e := readKeyed(filepath.Join(run, "judge_inputs.jsonl"), "id")
	if e != nil {
		return nil, nil, nil, e
	}
	answers, e := readKeyed(filepath.Join(run, "answers.jsonl"), "id")
	if e != nil {
		return nil, nil, nil, e
	}
	original, e := readKeyed(filepath.Join(run, "judgments.jsonl"), "id")
	if e != nil {
		return nil, nil, nil, e
	}
	manifest, e := oneRow(filepath.Join(run, "manifest.jsonl"))
	if e != nil {
		return nil, nil, nil, e
	}
	n := int(num(obj(manifest["config"])["questions"]))
	if n < 1 || len(packets) != 2*n || len(answers) != 2*n || len(original) != 2*n {
		return nil, nil, nil, fmt.Errorf("source run must have complete paired answers and DeepSeek judgments")
	}
	pairs := map[string]map[string]bool{}
	for id, p := range packets {
		qid, arm := str(p["question_id"]), str(p["arm"])
		a := answers[id]
		j := original[id+"/deepseek"]
		_, valid := j["correct"].(bool)
		if qid == "" || (arm != "baseline" && arm != "jev_filtered") || id != qid+"/"+arm || a == nil || p["submitted_answer"] != a["answer"] || a["question_id"] != qid || a["arm"] != arm || !valid || j["judge"] != "deepseek" {
			return nil, nil, nil, fmt.Errorf("invalid source packet %s", id)
		}
		if pairs[qid] == nil {
			pairs[qid] = map[string]bool{}
		}
		pairs[qid][arm] = true
	}
	if len(pairs) != n {
		return nil, nil, nil, fmt.Errorf("wrong question count")
	}
	for _, arms := range pairs {
		if len(arms) != 2 {
			return nil, nil, nil, fmt.Errorf("missing paired arm")
		}
	}
	return packets, original, manifest, nil
}
func (h *Harness) scoreSavedWithJev(id string, p M) error {
	payload := jevJudgePayload(h.C.Jev, p)
	stateHash := hash(canon(payload["state"]))
	if old, ok := h.S.get("judgments", id+"/jev"); ok {
		if old["input_sha256"] != stateHash {
			return fmt.Errorf("changed judge input %s", id)
		}
		return nil
	}
	key := "rescore/" + id
	r, e := h.guardedJev(key, "judge-jev-rescore", payload)
	if e != nil {
		return e
	}
	verdict := str(obj(obj(r["answers"])["evaluation"])["choice"])
	if (verdict != "correct" && verdict != "incorrect") || r["model"] != h.C.Jev {
		return fmt.Errorf("invalid Jev verdict/model for %s", id)
	}
	return h.S.put("judgments", M{"id": id + "/jev", "question_id": p["question_id"], "arm": p["arm"], "judge": "jev", "correct": verdict == "correct", "model_resolved": r["model"], "request_key": key, "input_sha256": stateHash})
}
func (h *Harness) rescoreReport(packets, original map[string]M) (M, error) {
	js := h.S.all("judgments")
	if len(js) != len(packets) {
		return nil, fmt.Errorf("incomplete Jev judgments")
	}
	scores, agreements := M{}, M{}
	byID := map[string]M{}
	disagreements := []M{}
	for _, j := range js {
		qid, arm := str(j["question_id"]), str(j["arm"])
		id := qid + "/" + arm
		p := packets[id]
		old := original[id+"/deepseek"]
		if p == nil || j["input_sha256"] != hash(canon(jevJudgePayload(h.C.Jev, p)["state"])) {
			return nil, fmt.Errorf("judgment input mismatch")
		}
		payload := jevJudgePayload(h.C.Jev, p)
		identity := hash(canon(M{"url": jevURL, "payload": payload}))
		receipt, ok := h.S.get("responses", "rescore/"+id+":"+identity)
		if !ok {
			return nil, fmt.Errorf("missing Jev response receipt")
		}
		r := obj(receipt["response"])
		choice := str(obj(obj(r["answers"])["evaluation"])["choice"])
		if r["model"] != h.C.Jev || (choice != "correct" && choice != "incorrect") || j["correct"] != (choice == "correct") {
			return nil, fmt.Errorf("verdict/receipt mismatch")
		}
		if j["correct"] == true {
			scores[arm] = num(scores[arm]) + 1
		}
		byID[id] = j
		if j["correct"] == old["correct"] {
			agreements[arm] = num(agreements[arm]) + 1
		} else {
			disagreements = append(disagreements, M{"question_id": qid, "arm": arm, "jev_correct": j["correct"], "deepseek_correct": old["correct"]})
		}
	}
	qs := []Question{}
	delta := map[string]float64{}
	wins, losses := 0, 0
	for id, p := range packets {
		if p["arm"] != "baseline" {
			continue
		}
		qid := str(p["question_id"])
		qs = append(qs, Question{ID: qid, DocID: str(obj(p["source"])["document_id"])})
		b, f := byID[id]["correct"] == true, byID[qid+"/jev_filtered"]["correct"] == true
		if f && !b {
			wins++
			delta[qid] = 1
		}
		if b && !f {
			losses++
			delta[qid] = -1
		}
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].ID < qs[j].ID })
	lo, hi := pairedBootstrap(qs, delta)
	rs := h.S.all("requests")
	failures := 0
	for _, r := range rs {
		if r["error"] != "" {
			failures++
		}
	}
	executions := h.S.all("executions")
	return M{"id": "jev-rescore-summary", "complete": true, "questions": len(qs), "judgments": len(js), "model": h.C.Jev, "workers": h.C.Workers, "scores": scores, "agreement_with_deepseek": agreements, "disagreements": disagreements, "filtered_only_correct": wins, "baseline_only_correct": losses, "filtered_minus_baseline_pp": 100 * float64(wins-losses) / float64(len(qs)), "paired_cluster_bootstrap_95_percent_pp": []float64{lo, hi}, "usage": summarizeRequests(rs), "failed_attempts": failures, "executions": executions, "interpretation": "Post-hoc supplementary judge; Jev also filtered the treatment evidence. Original DeepSeek scores remain the primary frozen results."}, nil
}
func addRescoreCommand(root *cobra.Command, run *string) {
	var out, model string
	var workers int
	var dry bool
	cmd := &cobra.Command{Use: "rescore-jev", Short: "Re-score saved answers with Jev in a separate resumable output directory", RunE: func(cmd *cobra.Command, args []string) error {
		started := time.Now()
		if workers < 1 || workers > 16 {
			return fmt.Errorf("workers must be between 1 and 16")
		}
		source, e := filepath.EvalSymlinks(*run)
		if e != nil {
			return e
		}
		source, _ = filepath.Abs(source)
		target, e := filepath.Abs(out)
		if e != nil {
			return e
		}
		if resolved, err := filepath.EvalSymlinks(target); err == nil {
			target = resolved
		} else if parent, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
			target = filepath.Join(parent, filepath.Base(target))
		}
		if target == source || strings.HasPrefix(target, source+string(os.PathSeparator)) || strings.HasPrefix(source, target+string(os.PathSeparator)) {
			return fmt.Errorf("output must be separate from the original run")
		}
		packets, original, manifest, e := rescoreInputs(source)
		if e != nil {
			return e
		}
		c := Config{}
		if e = json.Unmarshal(canon(manifest["config"]), &c); e != nil {
			return e
		}
		c.Jev = model
		c.Workers = workers
		c.MaxRequests = 3 * len(packets)
		c.Timeout = 180
		h := &Harness{C: c}
		ids := []string{}
		maxTokens, maxBytes := 0, 0
		for id, p := range packets {
			payload := jevJudgePayload(model, p)
			if !h.fitsJev(payload) {
				return fmt.Errorf("complete judge input %s exceeds Jev context guards; no truncation", id)
			}
			ids = append(ids, id)
			maxTokens = max(maxTokens, tokenCount(payload))
			maxBytes = max(maxBytes, len(canon(payload)))
		}
		sort.Strings(ids)
		if dry {
			fmt.Println(string(canon(M{"judgments": len(ids), "workers": workers, "max_proxy_tokens": maxTokens, "max_bytes": maxBytes, "dry_run": true})))
			return nil
		}
		if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
			if _, err = os.Stat(filepath.Join(target, "manifest.jsonl")); err != nil {
				return fmt.Errorf("nonempty output is not a rescore directory")
			}
		}
		s, e := newStore(target)
		if e != nil {
			return e
		}
		h.S = s
		lock, e := os.OpenFile(filepath.Join(target, "run.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer lock.Close()
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			return fmt.Errorf("another rescore writer is active")
		}
		hashes := M{}
		for _, file := range []string{"answers.jsonl", "judge_inputs.jsonl", "judgments.jsonl", "manifest.jsonl"} {
			digest, e := fileHash(filepath.Join(source, file))
			if e != nil {
				return e
			}
			hashes[file] = digest
		}
		frozen := M{"id": "rescore-protocol", "source_files_sha256": hashes, "model": model, "workers": workers, "max_requests_per_second": 16, "rubric": judgePrompt, "source_sha256": sourceHash(), "context_proxy_limit": c.JevPageTokens, "context_byte_limit": c.JevPageBytes, "judgments": len(ids)}
		if e = s.put("manifest", frozen); e != nil {
			return e
		}
		home, _ := os.UserHomeDir()
		_ = godotenv.Load(".env", filepath.Join(home, ".secrets/keys.env"))
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		h.Ctx = ctx
		h.HTTP = &http.Client{Timeout: time.Duration(c.Timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		h.requests = len(s.all("reservations"))
		before := h.requests
		batchStart := time.Now()
		ticker := time.NewTicker(time.Second / 16)
		defer ticker.Stop()
		err := h.parallel(ids, func(id string) error {
			if _, ok := s.get("judgments", id+"/jev"); !ok {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
			return h.scoreSavedWithJev(id, packets[id])
		})
		elapsed := time.Since(batchStart).Seconds()
		message := ""
		if err != nil {
			message = err.Error()
		}
		if e = s.put("executions", M{"id": fmt.Sprintf("execution-%03d", len(s.all("executions"))+1), "started_utc": started.UTC().Format(time.RFC3339Nano), "scoring_wall_seconds": elapsed, "command_wall_seconds_before_report": time.Since(started).Seconds(), "new_requests": h.requests - before, "workers": workers, "error": message}); e != nil {
			return e
		}
		if err != nil {
			return err
		}
		report, e := h.rescoreReport(packets, original)
		if e != nil {
			return e
		}
		report["context_maxima"] = M{"proxy_tokens": maxTokens, "serialized_bytes": maxBytes}
		if e = jsonFile(filepath.Join(target, "summary.jsonl"), report); e != nil {
			return e
		}
		fmt.Println(string(canon(report)))
		return nil
	}}
	cmd.Flags().StringVar(&out, "out", "", "Separate output directory (required)")
	_ = cmd.MarkFlagRequired("out")
	cmd.Flags().StringVar(&model, "model", "jev-1.13.0", "Pinned Jev judge model")
	cmd.Flags().IntVar(&workers, "workers", 8, "Concurrent requests (1–16), paced to at most 16 new requests/second")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "Validate all saved inputs and context sizes without inference or writes")
	root.AddCommand(cmd)
}
