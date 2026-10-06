package bench

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Study wires one study's plan, smoke test and report into a Cobra CLI.
type Study struct {
	Name   string
	Short  string
	Seed   uint64
	Plan   func(d *Data) ([]Job, M)
	Smoke  func() ([]Job, func([]Answer) error)
	Report func(d *Data, answers []Answer, out string) error
}

func (s Study) Command() *cobra.Command {
	root := &cobra.Command{Use: s.Name, Short: s.Short, SilenceUsage: true}
	dataDir := "data"
	sibling := "../jev-vs-rerankers/data"
	root.PersistentFlags().StringVar(&dataDir, "data", dataDir, "pinned TREC DL input directory")

	root.AddCommand(&cobra.Command{Use: "prepare", Short: "Fetch and verify pinned inputs (no model calls)", RunE: func(*cobra.Command, []string) error {
		if e := Prepare(dataDir, sibling); e != nil {
			return e
		}
		fmt.Println("inputs verified in", dataDir)
		return nil
	}})

	root.AddCommand(&cobra.Command{Use: "plan", Short: "Print the frozen request plan (no model calls)", RunE: func(*cobra.Command, []string) error {
		d, e := Load(dataDir)
		if e != nil {
			return e
		}
		jobs, _ := s.Plan(d)
		PrintPlan(jobs)
		return nil
	}})

	var out string
	var budget, rpm float64
	var workers, guard int
	run := &cobra.Command{Use: "run", Short: "Run or resume the benchmark (paid inference)", RunE: func(*cobra.Command, []string) error {
		d, e := Load(dataDir)
		if e != nil {
			return e
		}
		jobs, params := s.Plan(d)
		if e := Manifest(out, s.Name, jobs, params); e != nil {
			return e
		}
		r := &Runner{Out: out, Budget: budget, Workers: workers, RPM: rpm, Guard: guard}
		return r.Run(Interleave(jobs, s.Seed))
	}}
	run.Flags().StringVar(&out, "out", "results/"+s.Name+"-v1", "output directory")
	run.Flags().Float64Var(&budget, "budget", 10, "conservative USD reservation ceiling")
	run.Flags().Float64Var(&rpm, "rpm", 1000, "client-side request rate limit")
	run.Flags().IntVar(&workers, "workers", 16, "concurrent requests")
	run.Flags().IntVar(&guard, "guard", 200000, "maximum request bytes")
	root.AddCommand(run)

	smoke := &cobra.Command{Use: "smoke", Short: "Run synthetic smoke requests (paid, tiny)", RunE: func(*cobra.Command, []string) error {
		jobs, check := s.Smoke()
		dir := filepath.Join("results", s.Name+"-smoke-v1")
		r := &Runner{Out: dir, Budget: 0.05, Workers: 4, RPM: 300, Guard: guard}
		if e := r.Run(jobs); e != nil {
			return e
		}
		a, e := Replay(dir, jobs)
		if e != nil {
			return e
		}
		return check(a)
	}}
	root.AddCommand(smoke)

	status := &cobra.Command{Use: "status", Short: "Summarize completed requests (offline)", RunE: func(*cobra.Command, []string) error {
		d, e := Load(dataDir)
		if e != nil {
			return e
		}
		jobs, _ := s.Plan(d)
		a, e := Replay(out, jobs)
		if e != nil {
			return e
		}
		done := map[string]int{}
		total := map[string]int{}
		for _, x := range a {
			total[x.Job.Arm]++
			if x.OK {
				done[x.Job.Arm]++
			}
		}
		for _, k := range sortedKeys(total) {
			fmt.Printf("%-28s %6d / %6d\n", k, done[k], total[k])
		}
		return nil
	}}
	status.Flags().StringVar(&out, "out", "results/"+s.Name+"-v1", "output directory")
	root.AddCommand(status)

	report := &cobra.Command{Use: "report", Short: "Replay receipts and write the report (offline)", RunE: func(*cobra.Command, []string) error {
		d, e := Load(dataDir)
		if e != nil {
			return e
		}
		jobs, _ := s.Plan(d)
		a, e := Replay(out, jobs)
		if e != nil {
			return e
		}
		for _, x := range a {
			if !x.OK {
				return fmt.Errorf("incomplete run: %s has no successful receipt", x.Job.ID)
			}
		}
		return s.Report(d, a, out)
	}}
	report.Flags().StringVar(&out, "out", "results/"+s.Name+"-v1", "output directory")
	root.AddCommand(report)

	archive := &cobra.Command{Use: "archive", Short: "Pack receipts into receipts.jsonl.gz, sorted, for version control (offline)", RunE: func(*cobra.Command, []string) error {
		n, e := Archive(out)
		if e == nil {
			fmt.Printf("archived %d receipts\n", n)
		}
		return e
	}}
	archive.Flags().StringVar(&out, "out", "results/"+s.Name+"-v1", "output directory")
	root.AddCommand(archive)
	return root
}

func Main(s Study) {
	if e := s.Command().Execute(); e != nil {
		os.Exit(1)
	}
}

func PrintPlan(jobs []Job) {
	req := map[string]int{}
	dec := map[string]int{}
	bytes := map[string]int{}
	maxB := 0
	usd := 0.0
	for _, j := range jobs {
		req[j.Arm]++
		dec[j.Arm] += len(j.Decisions)
		bytes[j.Arm] += len(j.Body)
		if len(j.Body) > maxB {
			maxB = len(j.Body)
		}
		usd += UpperCost(j.Body)
	}
	fmt.Printf("%-28s %8s %9s %12s\n", "arm", "requests", "decisions", "MB")
	for _, k := range sortedKeys(req) {
		fmt.Printf("%-28s %8d %9d %12.2f\n", k, req[k], dec[k], float64(bytes[k])/1e6)
	}
	fmt.Printf("total requests %d; largest request %d bytes; conservative ceiling $%.2f\n", len(jobs), maxB, usd)
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Row is one scored decision with its request context.
type Row struct {
	Arm   string
	D     Decision
	P     float64
	Year  string
	JobID string
}

func Rows(answers []Answer) map[string][]Row {
	out := map[string][]Row{}
	for _, a := range answers {
		for _, d := range a.Job.Decisions {
			out[a.Job.Arm] = append(out[a.Job.Arm], Row{Arm: a.Job.Arm, D: d, P: a.Nouls[d.Key], Year: d.Query[:4], JobID: a.Job.ID})
		}
	}
	return out
}

func Judged(rows []Row) []Scored {
	var out []Scored
	for _, r := range rows {
		if r.D.Grade >= 0 {
			out = append(out, Scored{Query: r.D.Query, Year: r.Year, DocID: r.D.DocID, Grade: r.D.Grade, P: r.P})
		}
	}
	return out
}

// Resource summarizes measured requests per arm.
type Resource struct {
	Requests        int     `json:"requests"`
	Decisions       int     `json:"decisions"`
	InputTokens     int     `json:"input_tokens"`
	TokensPerDec    float64 `json:"input_tokens_per_decision"`
	ListUSD         float64 `json:"list_price_usd"`
	LatencyP50      float64 `json:"request_latency_p50_s"`
	LatencyP95      float64 `json:"request_latency_p95_s"`
	MsPerDecision   float64 `json:"request_ms_per_decision_p50"`
	RetriedRequests int     `json:"retried_requests"`
}

func Resources(answers []Answer) map[string]*Resource {
	lat := map[string][]float64{}
	out := map[string]*Resource{}
	for _, a := range answers {
		r := out[a.Job.Arm]
		if r == nil {
			r = &Resource{}
			out[a.Job.Arm] = r
		}
		r.Requests++
		r.Decisions += len(a.Job.Decisions)
		r.InputTokens += a.Tokens
		if a.Attempts > 1 {
			r.RetriedRequests++
		}
		lat[a.Job.Arm] = append(lat[a.Job.Arm], a.Seconds)
	}
	for k, r := range out {
		r.TokensPerDec = float64(r.InputTokens) / float64(r.Decisions)
		r.ListUSD = float64(r.InputTokens) * PricePerToken
		r.LatencyP50 = Percentile(lat[k], .5)
		r.LatencyP95 = Percentile(lat[k], .95)
		r.MsPerDecision = 1000 * r.LatencyP50 * float64(r.Requests) / float64(r.Decisions)
	}
	return out
}

// Markdown helpers.
func F(v float64, digits int) string {
	if math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%.*f", digits, v)
}

func CI(i Interval, digits int) string {
	return fmt.Sprintf("%s [%s, %s]", Signed(i.Est, digits), Signed(i.Lo, digits), Signed(i.Hi, digits))
}

func Signed(v float64, digits int) string {
	if math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%+.*f", digits, v)
}

func Table(header []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|")
	for range header {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
	}
	return b.String()
}

func ResourceTable(res map[string]*Resource, arms []string) string {
	var rows [][]string
	for _, a := range arms {
		r := res[a]
		if r == nil {
			continue
		}
		rows = append(rows, []string{"`" + a + "`", fmt.Sprint(r.Requests), fmt.Sprint(r.Decisions), F(r.TokensPerDec, 0), F(r.LatencyP50*1000, 0), F(r.LatencyP95*1000, 0), F(r.MsPerDecision, 1), fmt.Sprintf("$%.4f", r.ListUSD), fmt.Sprint(r.RetriedRequests)})
	}
	return Table([]string{"Arm", "Requests", "Decisions", "Input tokens / decision", "Request p50 ms", "Request p95 ms", "p50 request ms / decision", "List USD", "Retried"}, rows)
}

func Frontmatter(title, study string) string {
	return "---\ntitle: " + title + "\ntype: generated-study-report\nexperiment: jev-best-practices\nstudy: " + study + "\nauthor: Kyle Wild\n---\n\n"
}

// YearOf maps a query key like dl19-123 to its stratum.
func YearOf(q string) string { return q[:4] }

func QueryKeys(g map[string][]Scored) []string { return sortedKeys(g) }

// Archive writes every receipt (one compact JSON line, sorted by file name) to receipts.jsonl.gz.
// Receipts hold ids, payload hashes, status, latency and Jev answers; no request text.
func Archive(out string) (int, error) {
	files, e := filepath.Glob(filepath.Join(out, "receipts", "*.json"))
	if e != nil {
		return 0, e
	}
	sort.Strings(files)
	var buf bytes.Buffer
	z, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	for _, f := range files {
		raw, e := os.ReadFile(f)
		if e != nil {
			return 0, e
		}
		var c bytes.Buffer
		if e := json.Compact(&c, raw); e != nil {
			return 0, e
		}
		c.WriteByte('\n')
		z.Write(c.Bytes())
	}
	if e := z.Close(); e != nil {
		return 0, e
	}
	return len(files), AtomicWrite(filepath.Join(out, "receipts.jsonl.gz"), buf.Bytes())
}
