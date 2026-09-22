// Finalize a fully recorded supplementary selection without scoring an incomplete benchmark cohort.
package main

import (
	"fmt"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

func main() {
	var dir, planPath, root string
	cmd := &cobra.Command{Use: "finalizerecovery", Short: "Replay and finalize saved supplementary jobs offline; score only after merging with the source campaign", RunE: func(*cobra.Command, []string) (err error) {
		defer eval.Recover(&err)
		if dir == "" || planPath == "" || root == "" {
			return fmt.Errorf("run, plan and frozen root are required")
		}
		if _, e := os.Stat(filepath.Join(dir, "status-go.json")); e == nil {
			return fmt.Errorf("status already exists; refusing to overwrite")
		}
		plan := eval.ReadJSON(planPath).(map[string]any)
		if plan["protocol"] != "service-error-sensitivity-v1" {
			return fmt.Errorf("not a service recovery plan")
		}
		manifest := eval.ReadJSON(filepath.Join(dir, "manifest.json")).(map[string]any)
		pricing := eval.ReadJSON(filepath.Join(dir, "pricing.snapshot.json")).(map[string]any)
		for _, pair := range [][2]any{{manifest["jobs_sha256"], eval.Hash(eval.Canon(plan["jobs"]))}, {manifest["pricing_sha256"], eval.Hash(eval.Canon(pricing))}, {manifest["pricing_sha256"], eval.Hash(eval.Canon(plan["pricing"]))}, {eval.Hash(eval.Canon(manifest["config"])), eval.Hash(eval.Canon(plan["config"]))}} {
			if pair[0] != pair[1] {
				return fmt.Errorf("frozen execution provenance mismatch")
			}
		}
		replay, e := eval.Replay(root, dir)
		if e != nil {
			return e
		}
		if replay["complete"] != true || replay["identical_results"] != true || replay["identical_payloads"] != true {
			return fmt.Errorf("incomplete replay")
		}
		meter := eval.NewMeter(manifest["config"].(map[string]any), pricing)
		meter.Restore(dir)
		economics, e := eval.Economics(dir, pricing)
		if e != nil {
			return e
		}
		status := eval.M{"complete": true, "finished": replay["replayed_jobs"], "expected": replay["expected_jobs"], "budget": meter.Snapshot(), "scores": nil, "scores_unavailable": "Supplementary job selection is not a complete benchmark cohort; merge every retry outcome with original jobs before scoring.", "finalization": "offline replay and restored receipt accounting; original runner stopped during standalone paired-Ties scoring", "inference_calls": 0, "replay": replay, "finalizer_source_sha256": eval.Hash(eval.Read("cmd/finalizerecovery/main.go"))}
		eval.WriteJSON(filepath.Join(dir, "status-go.json"), status)
		fmt.Printf("Finalized %v/%v saved jobs; inference calls 0; economics jobs %v\n", status["finished"], status["expected"], economics["completed_jobs"])
		return nil
	}}
	cmd.Flags().StringVar(&dir, "run", "", "Saved supplementary run")
	cmd.Flags().StringVar(&planPath, "plan", "", "Frozen supplementary plan")
	cmd.Flags().StringVar(&root, "root", "", "Original frozen inference source root")
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
