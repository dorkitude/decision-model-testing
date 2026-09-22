package main

import (
	"encoding/json"
	"fmt"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/study"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() (err error) {
	defer eval.Recover(&err)
	v := viper.New()
	cmd := &cobra.Command{Use: "studyreport", Short: "Paired quality and resource analysis of completed campaign shards", RunE: func(*cobra.Command, []string) error {
		if v.GetBool("audit-question-split") {
			report, err := study.CampaignQuestionAudit(v.GetString("campaign"))
			if err != nil {
				return err
			}
			out := v.GetString("out")
			if err = os.MkdirAll(out, 0755); err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "question-split-audit.json"), report)
			fmt.Println("Audited full planned question split without model outcomes")
			return nil
		}
		if v.GetBool("audit-archives") {
			report, err := study.AuditCompletedArchives(v.GetString("campaign"))
			if err != nil {
				return err
			}
			out := v.GetString("out")
			if err = os.MkdirAll(out, 0755); err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "archive-audit.json"), report)
			fmt.Printf("Verified %v sealed shards / %v jobs; complete=%v\n", report["sealed_shards"], report["sealed_jobs"], report["complete"])
			return nil
		}
		rows, manifest, e := study.LoadCampaign(v.GetString("campaign"))
		if e != nil {
			return e
		}
		complete := len(rows) == int(manifest["jobs"].(float64))
		if retryDir := v.GetString("recovery-run"); retryDir != "" {
			plan := eval.ReadJSON(v.GetString("recovery-plan")).(map[string]any)
			retryRows, retryManifest, err := study.LoadRun(retryDir)
			if err != nil {
				return err
			}
			recovered := study.MergeServiceRecovery(rows, retryRows, plan, manifest, retryManifest)
			out := v.GetString("out")
			if err = os.MkdirAll(out, 0755); err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "service-recovery.json"), study.RecoveryReport(rows, recovered, v.GetInt("bootstrap")))
			if err = study.WriteRecords(filepath.Join(out, "service-recovery-judgments.csv"), recovered); err != nil {
				return err
			}
			fmt.Println("Reported original plus supplementary service-recovery policy")
			return nil
		}
		if v.GetBool("retry-plan") {
			plan := study.ServiceRetryPlan(v.GetString("campaign"), rows, manifest, complete)
			out := v.GetString("out")
			if _, err := os.Stat(out); err == nil {
				return fmt.Errorf("retry plan output directory already exists")
			}
			if err := os.MkdirAll(out, 0755); err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "retry-plan.json"), plan)
			eval.WriteJSON(filepath.Join(out, "jobs.json"), plan["jobs"])
			eval.WriteJSON(filepath.Join(out, "config.json"), plan["config"])
			eval.WriteJSON(filepath.Join(out, "pricing.json"), plan["pricing"])
			fmt.Println("Frozen supplementary service-error retry plan:", out)
			return nil
		}
		phase := v.GetString("cascade-phase")
		if phase != "none" {
			path := v.GetString("selection")
			if phase == "select" {
				selected := study.SelectCascades(rows, complete)
				raw, err := json.MarshalIndent(selected, "", "  ")
				if err != nil {
					return err
				}
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return err
				}
				_, err = f.Write(append(raw, '\n'))
				if err == nil {
					err = f.Sync()
				}
				closeErr := f.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				fmt.Println("Frozen development thresholds:", path)
				return nil
			}
			if phase != "evaluate" {
				return fmt.Errorf("unknown cascade phase %q", phase)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var selected eval.M
			if err = json.Unmarshal(raw, &selected); err != nil {
				return err
			}
			results := study.EvaluateCascades(rows, complete, selected, v.GetInt("bootstrap"))
			out := v.GetString("out")
			if err = os.MkdirAll(out, 0755); err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "cascades.json"), results)
			fmt.Println("Evaluated held-out cascade performance")
			return nil
		}
		out := v.GetString("out")
		if e = os.MkdirAll(out, 0755); e != nil {
			return e
		}
		if e = study.WriteRecords(filepath.Join(out, "judgments.csv"), rows); e != nil {
			return e
		}
		report := eval.M{"complete": len(rows) == int(manifest["jobs"].(float64)), "finished_jobs": len(rows), "expected_jobs": manifest["jobs"], "source_campaign": v.GetString("campaign"), "scope": "completed shards only; partial outputs are provisional", "scores": eval.Scores(rows), "calibration": study.Calibration(rows), "resources_and_strict_scores": study.StrictSummaries(rows), "paired_comparisons": study.PairedComparisons(rows, v.GetInt("bootstrap"))}
		eval.WriteJSON(filepath.Join(out, "quality.json"), report)
		eval.WriteJSON(filepath.Join(out, "disagreements.json"), study.Disagreements(rows, 2))
		eval.WriteJSON(filepath.Join(out, "reliability.json"), study.Reliability(rows, v.GetInt("bootstrap")))
		eval.WriteJSON(filepath.Join(out, "rating-sensitivity.json"), study.RatingSensitivity(rows, v.GetInt("bootstrap")))
		eval.WriteJSON(filepath.Join(out, "atomic-diagnostics.json"), study.AtomicDiagnostics(rows))
		if nativeDir := v.GetString("native-campaign"); nativeDir != "" {
			nativeRows, nativeManifest, err := study.LoadCampaign(nativeDir)
			if err != nil {
				return err
			}
			eval.WriteJSON(filepath.Join(out, "native-comparisons.json"), eval.M{"primary_complete": complete, "native_complete": len(nativeRows) == int(nativeManifest["jobs"].(float64)), "primary_jobs": len(rows), "native_jobs": len(nativeRows), "comparisons": study.NativeComparisons(rows, nativeRows, v.GetInt("bootstrap"))})
		}
		if e = study.WriteOverview(filepath.Join(out, "README.md"), report); e != nil {
			return e
		}
		raw, e := json.Marshal(eval.M{"finished_jobs": len(rows), "expected_jobs": manifest["jobs"], "complete": report["complete"]})
		if e != nil {
			return e
		}
		fmt.Println(string(raw))
		return nil
	}}
	cmd.Flags().String("campaign", "results/broad-published-v1", "Campaign directory")
	cmd.Flags().String("out", "studies/broad-published-v1/analysis", "Analysis output directory")
	cmd.Flags().Bool("audit-question-split", false, "Audit full planned input questions for development/held-out overlap; no model outcomes")
	cmd.Flags().Bool("audit-archives", false, "Verify sealed compressed evidence checksums and recorded replay completeness; no inference")
	cmd.Flags().String("recovery-run", "", "Completed supplementary run directory for recovered-policy analysis")
	cmd.Flags().String("recovery-plan", "", "Frozen service retry plan JSON")
	cmd.Flags().Bool("retry-plan", false, "Freeze a supplementary service-error retry plan after full campaign completion; no inference")
	cmd.Flags().String("native-campaign", "", "Optional native campaign for matched Compact/Atomic versus Vanilla comparisons")
	cmd.Flags().String("cascade-phase", "none", "none, select (complete campaign only), or evaluate (requires frozen selection)")
	cmd.Flags().String("selection", "cascade-selection.json", "Frozen development threshold file; select refuses overwrites")
	cmd.Flags().Int("bootstrap", 2000, "Paired bootstrap replicates")
	v.SetEnvPrefix("EE_STUDY")
	v.AutomaticEnv()
	v.BindPFlags(cmd.Flags())
	return cmd.Execute()
}
