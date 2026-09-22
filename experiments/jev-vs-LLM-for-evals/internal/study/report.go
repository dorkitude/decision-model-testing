package study

import (
	"fmt"
	"os"
	"strings"
)

func WriteOverview(path string, report M) error {
	var out strings.Builder
	fmt.Fprintf(&out, "# Evaluation Evaluation: quality and cost\n\nFinalized jobs: **%v / %v**. Complete: **%v**.\n\n", report["finished_jobs"], report["expected_jobs"], report["complete"])
	if !b(report["complete"]) {
		out.WriteString("**Provisional checkpoint.** Only completed shards are included. Small, unequal method samples and missing benchmark subsets prevent a final ranking.\n\n")
	}
	out.WriteString("Strict score gives zero credit to a job with any required-stage failure. It therefore measures end-to-end operation, including service availability; consult published scores and validity separately in [quality.json](quality.json). Scores below aggregate observed subsets only. LLMBar and RewardBench use equal subset weights; JudgeBench uses pooled weights. Ties companions remain one unit.\n\n")
	out.WriteString("Accounting USD includes estimated list-price charges plus retained reservations for unknown usage. It is not an invoice. Token counts cover reported usage only. Job latency includes helper calls, retries and both orders, under concurrent service load.\n\n")
	out.WriteString("[Every judgment and its cost](judgments.csv) · [Paired intervals and calibration](quality.json) · [Systematically selected disagreements](disagreements.json) · [Service reliability and jointly valid comparisons](reliability.json) · [Continuous-rating sensitivity](rating-sensitivity.json)\n\n")
	last := ""
	for _, r := range report["resources_and_strict_scores"].([]M) {
		group := s(r["benchmark"]) + " / " + s(r["method"])
		if group != last {
			fmt.Fprintf(&out, "\n## %s\n\n| Judge | Units | Valid jobs | Strict score | Accounting USD | USD/job | Input tokens | Output tokens | Known input USD | Known output USD | Unknown-cost attempts | Median job seconds |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n", group)
			last = group
		}
		model := strings.TrimPrefix(s(r["model"]), "accounts/fireworks/models/")
		fmt.Fprintf(&out, "| %s | %v | %v/%v | %.2f%% | $%.6f | $%.6f | %.0f | %.0f | $%.6f | $%.6f | %.0f | %.3f |\n", model, r["units"], r["valid_jobs"], r["jobs"], 100*n(r["strict_score"]), n(r["accounting_usd"]), n(r["accounting_usd_per_job"]), n(r["input_tokens"]), n(r["output_tokens"]), n(r["known_input_usd"]), n(r["known_output_usd"]), n(r["unknown_cost_attempts"]), n(r["native_wall_p50_s"]))
	}
	return os.WriteFile(path, []byte(out.String()), 0644)
}
