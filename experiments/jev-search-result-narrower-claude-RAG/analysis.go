package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func readRows(path string) ([]M, error) {
	rows := []M{}
	e := scan(path, func(b []byte) error {
		var r M
		if e := json.Unmarshal(b, &r); e != nil {
			return e
		}
		rows = append(rows, r)
		return nil
	})
	return rows, e
}
func readKeyed(path, field string) (map[string]M, error) {
	rows, e := readRows(path)
	if e != nil {
		return nil, e
	}
	m := map[string]M{}
	for _, r := range rows {
		id := str(r[field])
		if id == "" || m[id] != nil {
			return nil, fmt.Errorf("missing/duplicate %s", field)
		}
		m[id] = r
	}
	return m, nil
}
func oneRow(path string) (M, error) {
	r, e := readRows(path)
	if e != nil {
		return nil, e
	}
	if len(r) != 1 {
		return nil, fmt.Errorf("expected one row: %s", path)
	}
	return r[0], nil
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }
func addAnalysisCommands(root *cobra.Command, run *string) {
	var data, source string
	audit := &cobra.Command{Use: "audit", Short: "Verify paired coverage, raw receipts, source hashes, and disjoint sample offline", RunE: func(cmd *cobra.Command, args []string) error {
		r, e := auditEvidence(*run, data, source)
		if e == nil {
			fmt.Println(string(canon(r)))
		}
		return e
	}}
	audit.Flags().StringVar(&data, "data", "data", "Prepared data directory")
	audit.Flags().StringVar(&source, "source", ".", "Frozen harness source directory")
	root.AddCommand(audit)
	root.AddCommand(&cobra.Command{Use: "estimate-cost", Short: "Offline API-equivalent cost estimate; Max subscription is not a per-token invoice", RunE: func(cmd *cobra.Command, args []string) error {
		r, e := estimateCosts(*run)
		if e == nil {
			fmt.Println(string(canon(r)))
		}
		return e
	}})
}
func estimateCosts(run string) (M, error) {
	requests, e := readRows(filepath.Join(run, "requests.jsonl"))
	if e != nil {
		return nil, e
	}
	answers, e := readRows(filepath.Join(run, "answers.jsonl"))
	if e != nil {
		return nil, e
	}
	arms := M{}
	for _, arm := range []string{"baseline", "jev_filtered"} {
		searches, filters := map[string]bool{}, map[string]bool{}
		for _, a := range answers {
			if a["arm"] != arm {
				continue
			}
			for _, v := range arr(a["search_ids"]) {
				searches[str(v)] = true
			}
			for _, v := range arr(a["filter_ids"]) {
				filters[str(v)] = true
			}
		}
		costs := M{}
		counts := M{}
		tokensByModel := M{}
		total, claudeTotal := 0., 0.
		for _, r := range requests {
			key, phase := str(r["key"]), str(r["phase"])
			parts := strings.Split(key, "/")
			include := phase == "answer" && strings.Contains(key, "/"+arm+"/")
			if strings.HasPrefix(key, "search/") && len(parts) > 1 && searches[parts[1]] {
				include = true
			}
			if strings.HasPrefix(key, "filter/") && len(parts) > 3 && filters[parts[1]+"/"+parts[2]] {
				include = true
			}
			if !include {
				continue
			}
			model := str(obj(r["payload"])["model"])
			if model == "" {
				continue
			}
			in, out := usage(r)
			cost := 0.
			switch phase {
			case "answer":
				raw := obj(obj(r["response"])["claude_raw"])
				cost = num(raw["total_cost_usd"])
				claudeTotal += cost
			case "filter":
				cost = in * .042 / 1e6
			case "rerank":
				cost = in * .20 / 1e6
			case "embedding":
				cost = in * .10 / 1e6
			default:
				continue
			}
			costs[model] = num(costs[model]) + cost
			counts[model] = num(counts[model]) + 1
			tok := obj(tokensByModel[model])
			tok["input_tokens"] = num(tok["input_tokens"]) + in
			tok["output_tokens"] = num(tok["output_tokens"]) + out
			u := obj(obj(r["response"])["usage"])
			for _, field := range []string{"uncached_input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
				tok[field] = num(tok[field]) + num(u[field])
			}
			tokensByModel[model] = tok
			total += cost
		}
		arms[arm] = M{"estimated_api_equivalent_total_usd": total, "claude_cli_list_cost_usd": claudeTotal, "non_claude_model_cost_usd": total - claudeTotal, "cost_by_model_usd": costs, "requests_by_model": counts, "tokens_by_model": tokensByModel}
	}
	b := num(obj(arms["baseline"])["estimated_api_equivalent_total_usd"])
	f := num(obj(arms["jev_filtered"])["estimated_api_equivalent_total_usd"])
	return M{"id": "cost-estimate", "arms": arms, "estimated_savings_usd": b - f, "estimated_savings_percent": 100 * (b - f) / b, "rates_per_million_input_usd": M{"jev": .042, "reranker": .20, "embedding": .10}, "claude_cost_source": "CLI total_cost_usd / modelUsage costBasis=list, including reported cache usage", "billing": "Claude Max subscription; API-equivalent list cost is not incremental Max charges or an invoice. Subscription allocation unknown.", "excluded": "DeepSeek judging, preflight, Turbopuffer, index setup, hosting, taxes", "shared_search_allocation": "one allocation per arm using that search"}, nil
}
