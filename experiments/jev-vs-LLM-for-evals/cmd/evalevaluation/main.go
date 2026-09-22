package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dorkitude/decision-model-revolution/experiments/jev-vs-LLM-for-evals/internal/eval"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func execute() (err error) {
	defer eval.Recover(&err)
	v := viper.New()
	var configFile, root, out, jobsFile, pricingFile, save string
	cmd := &cobra.Command{Use: "evalevaluation", Short: "Jev vs LLM: accuracy, tokens, dollars, and latency", SilenceUsage: true, SilenceErrors: true}
	cmd.PersistentFlags().StringVar(&root, "root", ".", "Project root containing methodology sources")
	cmd.PersistentFlags().StringVar(&configFile, "config", "methodology/configs/validation.json", "JSON/YAML/TOML configuration")
	cmd.PersistentFlags().StringVar(&out, "out", "results/go-validation-v1", "Run directory")
	cmd.PersistentFlags().StringVar(&jobsFile, "jobs", "", "Frozen jobs JSON; preserve historical selections exactly")
	cmd.PersistentFlags().StringVar(&pricingFile, "pricing", "configs/pricing-2026-09-17.json", "Dated provider pricing JSON")
	cmd.PersistentFlags().Int("concurrency", 4, "Maximum concurrent jobs")
	cmd.PersistentFlags().Int("max-requests", 5000, "Hard API-attempt cap across resumes")
	cmd.PersistentFlags().Float64("timeout", 90, "HTTP request timeout in seconds")
	cmd.PersistentFlags().Int("max-tokens-floor", 8192, "Minimum completion-token budget")
	cmd.PersistentFlags().Int("per-subset", 0, "Sample units per subset; 0 selects full datasets (config may override)")
	for flag, key := range map[string]string{"concurrency": "concurrency", "max-requests": "max_requests", "timeout": "timeout_s", "max-tokens-floor": "max_tokens_floor", "per-subset": "per_subset"} {
		if e := v.BindPFlag(key, cmd.PersistentFlags().Lookup(flag)); e != nil {
			return e
		}
	}
	v.SetEnvPrefix("EE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	path := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(root, p)
	}
	load := func() eval.M {
		v.SetConfigFile(path(configFile))
		if e := v.ReadInConfig(); e != nil {
			panic(e)
		}
		settings := v.AllSettings()
		for _, key := range []string{"concurrency", "max_requests", "timeout_s", "max_tokens_floor", "per_subset", "shard_jobs", "max_estimated_usd"} {
			if raw, ok := settings[key].(string); ok {
				value, e := strconv.ParseFloat(raw, 64)
				if e != nil {
					panic(fmt.Errorf("invalid numeric configuration %s", key))
				}
				settings[key] = value
			}
		}
		b, e := json.Marshal(settings)
		if e != nil {
			panic(e)
		}
		var c eval.M
		if e = json.Unmarshal(b, &c); e != nil {
			panic(e)
		}
		eval.Validate(c)
		return c
	}
	plan := func(c eval.M) []eval.M {
		if jobsFile != "" {
			xs := eval.ReadJSON(path(jobsFile)).([]any)
			js := []eval.M{}
			for _, j := range xs {
				js = append(js, j.(map[string]any))
			}
			eval.VerifyJobs(js)
			return js
		}
		if e := eval.Prepare(root); e != nil {
			panic(e)
		}
		return eval.Plan(root, c)
	}
	printJSON := func(v any) {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			panic(e)
		}
		fmt.Println(string(b))
	}
	cmd.AddCommand(&cobra.Command{Use: "prepare", Short: "Fetch and verify pinned official benchmark data", RunE: func(*cobra.Command, []string) error { return eval.Prepare(root) }})
	pc := &cobra.Command{Use: "plan", Short: "Freeze deterministic jobs without inference", Run: func(*cobra.Command, []string) {
		js := plan(load())
		if save != "" {
			eval.WriteJSON(path(save), js)
		}
		printJSON(eval.M{"jobs": len(js), "jobs_sha256": eval.Hash(eval.Canon(js)), "sampling": "sha256-v1; explicit --jobs preserves historical sampling", "saved_to": save})
	}}
	pc.Flags().StringVar(&save, "save", "", "Write selected jobs JSON")
	cmd.AddCommand(pc)
	cmd.AddCommand(&cobra.Command{Use: "run", Short: "Run native Go benchmark workflows with durable receipts", RunE: func(cmd *cobra.Command, _ []string) error {
		c := load()
		return eval.Run(cmd.Context(), root, path(out), c, plan(c), eval.ReadJSON(path(pricingFile)).(map[string]any))
	}})
	cmd.AddCommand(&cobra.Command{Use: "replay", Short: "Verify every saved payload and verdict in Go with zero inference", RunE: func(*cobra.Command, []string) error {
		r, e := eval.Replay(root, path(out))
		if e == nil {
			printJSON(r)
		}
		return e
	}})
	cmd.AddCommand(&cobra.Command{Use: "report", Short: "Recompute accuracy, token, cost and latency reports from receipts", RunE: func(*cobra.Command, []string) error {
		r, e := eval.Economics(path(out), eval.ReadJSON(path(pricingFile)).(map[string]any))
		if e != nil {
			return e
		}
		s := eval.Summarize(path(out))
		if e = os.WriteFile(filepath.Join(path(out), "ECONOMICS.md"), []byte(eval.EconomicMarkdown(r)), 0644); e != nil {
			return e
		}
		printJSON(eval.M{"complete": s["complete"], "jobs": r["completed_jobs"], "economics": filepath.Join(path(out), "economics.json"), "totals": r["totals"]})
		return nil
	}})
	campaign := &cobra.Command{Use: "campaign", Short: "Plan and execute a resumable, sharded full study"}
	campaign.AddCommand(&cobra.Command{Use: "plan", RunE: func(*cobra.Command, []string) error {
		r, e := eval.CampaignPlan(root, path(out), load(), eval.ReadJSON(path(pricingFile)).(map[string]any))
		if e == nil {
			printJSON(eval.M{"jobs": r["jobs"], "shards": len(r["shards"].([]eval.M)), "benchmark_jobs": r["benchmark_jobs"]})
		}
		return e
	}})
	campaign.AddCommand(&cobra.Command{Use: "run", RunE: func(cmd *cobra.Command, _ []string) error { return eval.CampaignRun(cmd.Context(), root, path(out)) }})
	cmd.AddCommand(campaign)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cmd.ExecuteContext(ctx)
}
