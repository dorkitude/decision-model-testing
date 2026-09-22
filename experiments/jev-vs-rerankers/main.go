package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sort"
)

type M = map[string]any

var methods = []string{"qwen", "jev-noul", "jev-grade", "jev-ordinal"}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func encode(v any) []byte {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		panic(e)
	}
	return append(b, '\n')
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func writeJSON(path string, v any) error { return atomicWrite(path, encode(v)) }
func atomicWrite(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func fileHash(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}
func sourceHash() (string, error) {
	files, e := filepath.Glob("*.go")
	if e != nil {
		return "", e
	}
	files = append(files, "go.mod", "go.sum", "PROTOCOL-v1.md", "prompts.json", "PROMPT_STUDY.md")
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		b, e := os.ReadFile(p)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%s\x00%s\x00", p, b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func main() {
	var out, from, study string
	var pairs int
	var limit int
	var maxCost float64
	root := &cobra.Command{Use: "rerankbench", Short: "Reproducible TREC DL Jev versus Qwen reranking benchmark", SilenceUsage: true, PersistentPreRunE: func(*cobra.Command, []string) error { return selectStudy(study) }}
	root.PersistentFlags().StringVar(&study, "study", "initial", "Method roster: initial or prompts")
	root.PersistentFlags().StringVar(&out, "out", "results/trec-v1", "Evidence output directory")
	root.AddCommand(&cobra.Command{Use: "prepare", Short: "Download and verify pinned benchmark inputs; build NIST trec_eval (no inference)", RunE: func(*cobra.Command, []string) error { return prepare() }})
	root.AddCommand(&cobra.Command{Use: "smoke", Short: "Run three synthetic relevance checks through every model interface", RunE: func(*cobra.Command, []string) error { return smoke(out, maxCost) }})
	run := &cobra.Command{Use: "run", Short: "Run all 97 queries, 100 candidates and selected methods; resume immutable completed requests", RunE: func(*cobra.Command, []string) error { return runBenchmark(out, pairs, limit, maxCost) }}
	run.Flags().IntVar(&pairs, "workers", 16, "Maximum simultaneous Jev passage requests within one query")
	run.Flags().IntVar(&limit, "limit", 0, "Optional query limit for a separately labeled diagnostic; 0 means full benchmark")
	root.PersistentFlags().Float64Var(&maxCost, "max-cost", 20, "Conservative cumulative USD reservation ceiling for this output directory")
	root.AddCommand(run)
	replayCmd := &cobra.Command{Use: "replay", Short: "Create a new offline analysis from immutable provider receipts", RunE: func(*cobra.Command, []string) error { return replay(from, out) }}
	replayCmd.Flags().StringVar(&from, "from", "results/trec-v1", "Original evidence directory")
	root.AddCommand(replayCmd)
	seed := &cobra.Command{Use: "seed-baseline", Short: "Copy immutable baseline evidence for the prompt study, without inference", RunE: func(*cobra.Command, []string) error { return seedBaseline(from, out) }}
	seed.Flags().StringVar(&from, "from", "results/trec-v1-canonical", "Audited original baseline directory")
	root.AddCommand(seed)

	root.AddCommand(&cobra.Command{Use: "report", Short: "Recompute metrics, paired intervals and resource tables offline", RunE: func(*cobra.Command, []string) error { return report(out) }})
	root.AddCommand(&cobra.Command{Use: "audit", Short: "Replay request scores, verify coverage/hashes and compare every metric with trec_eval", RunE: func(*cobra.Command, []string) error { return audit(out) }})
	if e := root.Execute(); e != nil {
		os.Exit(1)
	}
}
