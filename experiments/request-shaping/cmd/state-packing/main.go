// Command state-packing runs Study 1: one item per state versus several items sharing a state.
package main

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	b "github.com/dorkitude/decision-model-testing/experiments/request-shaping/internal/bench"
)

const seed = 20260925

var packSizes = []int{2, 4, 10, 20, 50, 100}

func key(i int) string { return fmt.Sprintf("p%03d", i+1) }

// Frozen reference wrappers; each precedes the frozen relevance prompt.
func keyPrompt(k string) string {
	return "Evaluate only the passage at `passages." + k + "`; ignore the other passages. " + b.Relevance
}
func indexPrompt(i int) string {
	return fmt.Sprintf("Evaluate only the passage at `passages[%d]`; ignore the other passages. ", i) + b.Relevance
}
func idPrompt(id string) string {
	return "Evaluate only the passage whose `id` is \"" + id + "\"; ignore the other passages. " + b.Relevance
}
func crossPrompt(k string) string {
	return "Evaluate only `items." + k + "`: judge its passage against its own question; ignore the other items. " + b.Relevance
}

func dec(k string, it b.Item, pos, size int, tag string) b.Decision {
	return b.Decision{Key: k, Ref: it.Ref(), Query: it.QueryKey, DocID: it.DocID, Grade: it.Grade, Pos: pos, Size: size, Tag: tag}
}

// Atomic reproduces the frozen jev-vs-rerankers jev-noul request byte for byte.
func atomic(id, arm string, it b.Item) b.Job {
	req := b.Request(b.M{"passage": it.Text, "question": it.QueryText}, b.Obj{b.P("useful", b.Noul(b.Relevance))})
	return b.NewJob(id, arm, req, []b.Decision{dec("useful", it, 0, 1, "")})
}

// keyPack puts items under named keys; ask selects which items receive questions.
func keyPack(id, arm string, items []b.Item, ask func(int) bool, tag string) b.Job {
	passages := b.Obj{}
	qs := b.Obj{}
	var ds []b.Decision
	for i, it := range items {
		passages = append(passages, b.P(key(i), it.Text))
		if ask(i) {
			qs = append(qs, b.P(key(i), b.Noul(keyPrompt(key(i)))))
			ds = append(ds, dec(key(i), it, i, len(items), tag))
		}
	}
	req := b.Request(b.M{"passages": passages, "question": items[0].QueryText}, qs)
	return b.NewJob(id, arm, req, ds)
}

func all(int) bool { return true }

// packs splits each query's candidates into seeded groups of k.
func packs(q *b.Query, k int, perm int) [][]b.Item {
	items := b.Shuffled(q.Items, b.RNG(seed, fmt.Sprintf("perm%d-%s", perm, q.Key)))
	var out [][]b.Item
	for i := 0; i < len(items); i += k {
		out = append(out, items[i:i+k])
	}
	return out
}

// crossPacks forms packs of k items from k distinct queries, draining queries evenly.
func crossPacks(d *b.Data, k int) [][]b.Item {
	r := b.RNG(seed, fmt.Sprintf("cross%d", k))
	left := map[string][]b.Item{}
	var keys []string
	for _, q := range d.Queries {
		left[q.Key] = b.Shuffled(q.Items, r)
		keys = append(keys, q.Key)
	}
	var out [][]b.Item
	for {
		keys = b.Shuffled(keys, r)
		sort.SliceStable(keys, func(i, j int) bool { return len(left[keys[i]]) > len(left[keys[j]]) })
		if len(left[keys[0]]) == 0 {
			return out
		}
		if len(left[keys[k-1]]) == 0 {
			panic("cross packing cannot keep queries distinct")
		}
		var p []b.Item
		for _, q := range keys[:k] {
			p = append(p, left[q][0])
			left[q] = left[q][1:]
		}
		out = append(out, p)
	}
}

func plan(d *b.Data) ([]b.Job, b.M) {
	var jobs []b.Job
	sub := d.Subset(10, seed)
	subset := b.QuerySet(sub)
	for _, q := range d.Queries {
		for i, it := range q.Items {
			jobs = append(jobs, atomic(fmt.Sprintf("atomic-%s-%03d", q.Key, i), "atomic", it))
			if subset[q.Key] {
				jobs = append(jobs, atomic(fmt.Sprintf("retest-%s-%03d", q.Key, i), "retest", it))
			}
		}
		for _, k := range packSizes {
			for perm := 1; perm <= 2; perm++ {
				for pi, p := range packs(&q, k, perm) {
					arm := fmt.Sprintf("pack-%d-p%d", k, perm)
					jobs = append(jobs, keyPack(fmt.Sprintf("%s-%s-%02d", arm, q.Key, pi), arm, p, all, ""))
				}
			}
		}
		for pi, p := range packs(&q, 10, 1) {
			rev := make([]b.Item, len(p))
			for i := range p {
				rev[i] = p[len(p)-1-i]
			}
			jobs = append(jobs, keyPack(fmt.Sprintf("swap-10-%s-%02d", q.Key, pi), "swap-10", rev, all, ""))
			jobs = append(jobs, refPack(fmt.Sprintf("ref-index-10-%s-%02d", q.Key, pi), "ref-index-10", p, false))
			jobs = append(jobs, refPack(fmt.Sprintf("ref-id-10-%s-%02d", q.Key, pi), "ref-id-10", p, true))
		}
		if subset[q.Key] {
			for _, k := range []int{10, 50} {
				for pi, p := range packs(&q, k, 1) {
					for t := range p {
						arm := fmt.Sprintf("single-%d", k)
						jobs = append(jobs, keyPack(fmt.Sprintf("%s-%s-%02d-%03d", arm, q.Key, pi, t), arm, p, func(i int) bool { return i == t }, ""))
					}
				}
			}
		}
		jobs = append(jobs, neighborJobs(&q)...)
	}
	for _, k := range []int{4, 20} {
		for pi, p := range crossPacks(d, k) {
			jobs = append(jobs, crossJob(fmt.Sprintf("cross-%d-%04d", k, pi), fmt.Sprintf("cross-%d", k), p))
		}
	}
	return jobs, b.M{"seed": seed, "pack_sizes": packSizes, "permutations": 2, "subset_queries": keys(sub), "prompts": b.M{"key": keyPrompt("p001"), "index": indexPrompt(0), "id": idPrompt("ID"), "cross": crossPrompt("i001")}}
}

func keys(qs []*b.Query) []string {
	var out []string
	for _, q := range qs {
		out = append(out, q.Key)
	}
	return out
}

func refPack(id, arm string, items []b.Item, byID bool) b.Job {
	qs := b.Obj{}
	var ds []b.Decision
	var passages []any
	for i, it := range items {
		k := fmt.Sprintf("q%03d", i+1)
		if byID {
			passages = append(passages, b.Obj{b.P("id", it.DocID), b.P("text", it.Text)})
			qs = append(qs, b.P(k, b.Noul(idPrompt(it.DocID))))
		} else {
			passages = append(passages, it.Text)
			qs = append(qs, b.P(k, b.Noul(indexPrompt(i))))
		}
		ds = append(ds, dec(k, it, i, len(items), ""))
	}
	return b.NewJob(id, arm, b.Request(b.M{"passages": passages, "question": items[0].QueryText}, qs), ds)
}

func crossJob(id, arm string, items []b.Item) b.Job {
	state := b.Obj{}
	qs := b.Obj{}
	var ds []b.Decision
	for i, it := range items {
		k := fmt.Sprintf("i%03d", i+1)
		state = append(state, b.P(k, b.M{"passage": it.Text, "question": it.QueryText}))
		qs = append(qs, b.P(k, b.Noul(crossPrompt(k))))
		ds = append(ds, dec(k, it, i, len(items), ""))
	}
	return b.NewJob(id, arm, b.Request(b.M{"items": state}, qs), ds)
}

// neighborJobs packs a judged target with four relevant or four non-relevant
// neighbors from its own query, at the same seeded slot, asking only about the target.
func neighborJobs(q *b.Query) []b.Job {
	var rel, non []b.Item
	for _, it := range q.Items {
		if it.Grade >= 2 {
			rel = append(rel, it)
		} else if it.Grade == 0 {
			non = append(non, it)
		}
	}
	r := b.RNG(seed, "neighbors-"+q.Key)
	var targets []b.Item
	for _, group := range [][]b.Item{rel, non} {
		g := b.Shuffled(group, r)
		if len(g) > 10 {
			g = g[:10]
		}
		targets = append(targets, g...)
	}
	var jobs []b.Job
	for _, t := range targets {
		others := func(xs []b.Item) []b.Item {
			var o []b.Item
			for _, x := range b.Shuffled(xs, r) {
				if x.DocID != t.DocID && len(o) < 4 {
					o = append(o, x)
				}
			}
			return o
		}
		nr, nn := others(rel), others(non)
		if len(nr) < 4 || len(nn) < 4 {
			continue
		}
		slot := r.IntN(5)
		for _, c := range []struct {
			arm string
			nb  []b.Item
		}{{"neighbor-rel", nr}, {"neighbor-non", nn}} {
			items := append([]b.Item{}, c.nb[:slot]...)
			items = append(items, t)
			items = append(items, c.nb[slot:]...)
			jobs = append(jobs, keyPack(fmt.Sprintf("%s-%s-%s", c.arm, q.Key, t.DocID), c.arm, items, func(i int) bool { return i == slot }, ""))
		}
	}
	return jobs
}

func smoke() ([]b.Job, func([]b.Answer) error) {
	q := "What temperature does water boil at sea level?"
	mk := func(id, text string) b.Item {
		return b.Item{QueryKey: "smoke-1", QueryText: q, DocID: id, Text: text, Grade: -1}
	}
	yes := mk("yes", "At sea level, pure water boils at 100 degrees Celsius (212 degrees Fahrenheit).")
	no1 := mk("no1", "The Eiffel Tower was completed in 1889 for the World's Fair in Paris.")
	no2 := mk("no2", "Basketball teams have five players on the court at a time.")
	no3 := mk("no3", "Tulips are spring-blooming perennial herbaceous bulbiferous geophytes.")
	jobs := []b.Job{
		atomic("atomic-yes", "atomic", yes), atomic("atomic-no", "atomic", no1),
		keyPack("pack-4", "pack", []b.Item{no1, no2, yes, no3}, all, ""),
		refPack("ref-index-4", "ref-index", []b.Item{no1, yes, no2, no3}, false),
		refPack("ref-id-4", "ref-id", []b.Item{no1, no2, no3, yes}, true),
		crossJob("cross-2", "cross", []b.Item{yes, {QueryKey: "smoke-2", QueryText: "Who designed the Eiffel Tower?", DocID: "no1", Text: no1.Text, Grade: -1}}),
	}
	return jobs, func(as []b.Answer) error {
		for _, a := range as {
			var hi, lo []float64
			for _, d := range a.Job.Decisions {
				p := a.Nouls[d.Key]
				if d.DocID == "yes" {
					hi = append(hi, p)
				} else if d.Query == "smoke-1" {
					lo = append(lo, p)
				}
				fmt.Printf("%-12s %-5s %s %.2f\n", a.Job.ID, d.Key, d.DocID, p)
			}
			for _, h := range hi {
				for _, l := range lo {
					if h <= l {
						return fmt.Errorf("%s: answer passage did not outscore an unrelated one", a.Job.ID)
					}
				}
			}
		}
		fmt.Println("smoke passed: the answer passage outranks unrelated passages in every request shape")
		return nil
	}
}

func main() {
	b.Main(b.Study{Name: "state-packing", Short: "Study 1: atomic versus packed Jev state", Seed: seed, Plan: plan, Smoke: smoke, Report: report})
}

func report(d *b.Data, answers []b.Answer, out string) error {
	rows := b.Rows(answers)
	sub := b.QuerySet(d.Subset(10, seed))
	cond := func(name string, arms ...string) *b.Condition { return b.NewCondition(d, rows, name, arms...) }
	atom := cond("atomic", "atomic")
	ref := atom.RefScores()
	retest := b.DeltaVs(rows["retest"], ref)
	var md strings.Builder
	md.WriteString(b.Frontmatter("Study 1 results: state packing", "state-packing"))
	md.WriteString("# Study 1 results: state packing\n\nGenerated by `state-packing report` from saved receipts. `jev-1.13.0`, TREC DL19+DL20 (97 queries × 100 BM25 candidates), frozen Noul relevance prompt. Intervals are 95% query-cluster bootstrap intervals stratified by year (nDCG 10,000 replicates; item metrics 2,000). Holm adjustment covers the six primary pack-size contrasts. Δp compares each decision with the same item's fresh atomic probability.\n\n")

	// Noise floor and drift.
	fz, e := b.Frozen(d, "jev-noul", []string{"useful"})
	if e != nil {
		return e
	}
	md.WriteString("## Noise floor\n\n")
	noise := [][]string{{"Fresh retest vs. fresh atomic (20-query subset)", fmt.Sprint(retest.N), b.F(retest.MeanAbs, 4), b.Signed(retest.Signed, 4), b.F(100*retest.Changed, 2) + "%", b.F(100*retest.Flipped, 2) + "%"}}
	identical := 0
	if fz != nil {
		frozen := map[string]float64{}
		for _, a := range answers {
			if a.Job.Arm != "atomic" {
				continue
			}
			dd := a.Job.Decisions[0]
			if f, ok := fz[dd.Ref]; ok {
				frozen[dd.Ref] = f.Nouls["useful"]
				if f.PayloadSHA == b.Digest(a.Job.Body) {
					identical++
				}
			}
		}
		fd := b.DeltaVs(rows["atomic"], frozen)
		noise = append(noise, []string{"Fresh atomic vs. frozen 2026-09-18 `trec-v1` run", fmt.Sprint(fd.N), b.F(fd.MeanAbs, 4), b.Signed(fd.Signed, 4), b.F(100*fd.Changed, 2) + "%", b.F(100*fd.Flipped, 2) + "%"})
	}
	md.WriteString(b.Table([]string{"Comparison", "Decisions", "Mean \\|Δp\\|", "Mean Δp", "\\|Δp\\| > 0.05", "Crosses 0.5"}, noise))
	if fz != nil {
		fmt.Fprintf(&md, "\n%d of 9,700 fresh atomic request bodies are byte-identical (SHA-256) to the frozen run's requests.\n", identical)
	}

	// Primary: pack size.
	md.WriteString("\n## Pack size\n\nEach item appears in two seeded packs per size (different neighbors and slots); nDCG averages the two permutations per query. `pack-100` puts the whole candidate list in one request.\n\n")
	var prim [][]string
	var ps []float64
	var intervals []b.Interval
	type line struct {
		name string
		c    *b.Condition
	}
	var lines []line
	for _, k := range packSizes {
		lines = append(lines, line{fmt.Sprintf("pack-%d", k), cond(fmt.Sprintf("pack-%d", k), fmt.Sprintf("pack-%d-p1", k), fmt.Sprintf("pack-%d-p2", k))})
	}
	for _, l := range lines {
		iv := b.NDCGDiff(l.c, atom, 10000, "ndcg-"+l.name)
		ps = append(ps, iv.P)
		intervals = append(intervals, iv)
	}
	holm := b.Holm(ps)
	prim = append(prim, []string{"`atomic`", "1", b.F(atom.MeanNDCG(), 4), "—", "—", b.F(b.AUC(atom.Items), 4), "—", b.F(b.Brier(atom.Items), 4), b.F(b.ECE(atom.Items), 4), "—", "—"})
	for i, l := range lines {
		dl := b.DeltaVs(l.c.Rows, ref)
		auc := b.MetricDiff(l.c.Items, atom.Items, b.AUC, 2000, "auc-"+l.name)
		prim = append(prim, []string{"`" + l.name + "`", fmt.Sprint(packSizes[i]), b.F(l.c.MeanNDCG(), 4), b.CI(intervals[i], 4), b.F(holm[i], 4), b.F(b.AUC(l.c.Items), 4), b.CI(auc, 4), b.F(b.Brier(l.c.Items), 4), b.F(b.ECE(l.c.Items), 4), b.F(dl.MeanAbs, 4), b.Signed(dl.Signed, 4)})
	}
	md.WriteString(b.Table([]string{"Arm", "K", "nDCG@10", "Δ vs atomic", "Holm p", "AUC (grade≥2)", "Δ AUC", "Brier", "ECE", "Mean \\|Δp\\|", "Mean Δp"}, prim))
	md.WriteString("\nTie sensitivity: Jev returns probabilities rounded to 0.01, and ties above use BM25 order. Averaging nDCG@10 over 50 seeded random orderings of tied scores instead:\n\n")
	atomR := atom.RandomTies(d, 50)
	var tr [][]string
	tr = append(tr, []string{"`atomic`", b.F(atomR.MeanNDCG(), 4), "—"})
	for _, l := range lines {
		c := l.c.RandomTies(d, 50)
		tr = append(tr, []string{"`" + l.name + "`", b.F(c.MeanNDCG(), 4), b.CI(b.NDCGDiff(c, atomR, 10000, "ties-"+l.name), 4)})
	}
	md.WriteString(b.Table([]string{"Arm", "nDCG@10, random ties", "Δ vs atomic"}, tr))

	// Other arms.
	md.WriteString("\n## Pack shape, reference style, and order\n\nAll at K = 10 (permutation 1) unless named. `cross-K` packs K items from K different queries, each with its own question. `swap-10` reverses each `pack-10-p1` pack. Δ columns compare with `atomic` on the same items.\n\n")
	var other [][]string
	p10 := cond("pack-10-p1", "pack-10-p1")
	for _, c := range []*b.Condition{p10, cond("ref-index-10", "ref-index-10"), cond("ref-id-10", "ref-id-10"), cond("swap-10", "swap-10"), cond("cross-4", "cross-4"), cond("cross-20", "cross-20")} {
		dl := b.DeltaVs(c.Rows, ref)
		nd := b.NDCGDiff(c, atom, 10000, "ndcg-"+c.Name)
		auc := b.MetricDiff(c.Items, atom.Items, b.AUC, 2000, "auc-"+c.Name)
		other = append(other, []string{"`" + c.Name + "`", b.F(c.MeanNDCG(), 4), b.CI(nd, 4), b.F(b.AUC(c.Items), 4), b.CI(auc, 4), b.F(b.ECE(c.Items), 4), b.F(dl.MeanAbs, 4), b.Signed(dl.Signed, 4), b.F(100*dl.Flipped, 2) + "%"})
	}
	md.WriteString(b.Table([]string{"Arm", "nDCG@10", "Δ vs atomic", "AUC", "Δ AUC", "ECE", "Mean \\|Δp\\|", "Mean Δp", "Crosses 0.5"}, other))
	sw := b.DeltaVs(rows["swap-10"], p10.RefScores())
	fmt.Fprintf(&md, "\nSwap test: reversing pack order changes an item's probability by mean \\|Δp\\| = %s relative to the unreversed pack (retest noise %s); %s%% of decisions cross 0.5.\n", b.F(sw.MeanAbs, 4), b.F(retest.MeanAbs, 4), b.F(100*sw.Flipped, 2))

	// Distraction versus fan-out.
	md.WriteString("\n## Distraction versus question fan-out\n\n`single-K` sends the same state as `pack-K-p1` but asks only about one passage. Twenty-query subset.\n\n")
	var sr [][]string
	subAtom := b.Restrict(atom.Items, sub)
	for _, k := range []int{10, 50} {
		pk := cond(fmt.Sprintf("pack-%d-p1", k), fmt.Sprintf("pack-%d-p1", k))
		var pkRows []b.Row
		for _, r := range pk.Rows {
			if sub[r.D.Query] {
				pkRows = append(pkRows, r)
			}
		}
		sg := cond(fmt.Sprintf("single-%d", k), fmt.Sprintf("single-%d", k))
		for _, x := range []struct {
			name string
			rows []b.Row
			it   []b.Scored
		}{{"atomic (subset)", nil, subAtom}, {pk.Name + " (subset)", pkRows, b.Restrict(pk.Items, sub)}, {sg.Name, sg.Rows, sg.Items}} {
			if x.rows == nil && k == 50 {
				continue
			}
			dl := b.DeltaVs(x.rows, ref)
			mabs, sgn := b.F(dl.MeanAbs, 4), b.Signed(dl.Signed, 4)
			if x.rows == nil {
				mabs, sgn = "—", "—"
			}
			sr = append(sr, []string{"`" + x.name + "`", b.F(b.AUC(x.it), 4), b.F(b.ECE(x.it), 4), mabs, sgn})
		}
	}
	md.WriteString(b.Table([]string{"Arm", "AUC", "ECE", "Mean \\|Δp\\| vs atomic", "Mean Δp"}, sr))

	// Position within packs.
	md.WriteString("\n## Position within the pack\n\nMean Δp versus atomic by the item's slot, pooled over both permutations. Fifths of the pack for K ≥ 10.\n\n")
	var pr [][]string
	for _, k := range []int{10, 20, 50, 100} {
		c := lines[indexOf(packSizes, k)].c
		row := []string{fmt.Sprintf("`pack-%d`", k)}
		for f := 0; f < 5; f++ {
			var rs []b.Row
			for _, r := range c.Rows {
				if r.D.Pos*5/r.D.Size == f {
					rs = append(rs, r)
				}
			}
			dl := b.DeltaVs(rs, ref)
			row = append(row, b.Signed(dl.Signed, 3)+" / "+b.F(dl.MeanAbs, 3)+" / AUC "+b.F(b.AUC(b.Judged(rs)), 3))
		}
		pr = append(pr, row)
	}
	md.WriteString(b.Table([]string{"Arm", "First fifth", "Second", "Middle", "Fourth", "Last fifth"}, pr))
	md.WriteString("\nCells: mean Δp / mean \\|Δp\\| / AUC.\n")

	// Neighbor relevance.
	md.WriteString("\n## Neighbor relevance\n\nA judged target packed with four relevant (grade ≥ 2) or four non-relevant (grade 0) passages from its own query, at the same seeded slot; only the target is asked about.\n\n")
	nrel := map[string]float64{}
	for _, r := range rows["neighbor-rel"] {
		nrel[r.D.Ref] = r.P
	}
	var nb [][]string
	for _, tgt := range []struct {
		name string
		ok   func(int) bool
	}{{"relevant targets", func(g int) bool { return g >= 2 }}, {"non-relevant targets", func(g int) bool { return g == 0 }}, {"all targets", func(int) bool { return true }}} {
		var sel []b.Row
		for _, r := range rows["neighbor-non"] {
			if tgt.ok(r.D.Grade) {
				sel = append(sel, r)
			}
		}
		iv := b.PairedRows(sel, func(r b.Row) (float64, bool) { v, ok := nrel[r.D.Ref]; return v - r.P, ok }, 2000, "neighbor-"+tgt.name)
		var relRows []b.Row
		for _, r := range rows["neighbor-rel"] {
			if tgt.ok(r.D.Grade) {
				relRows = append(relRows, r)
			}
		}
		nb = append(nb, []string{tgt.name, fmt.Sprint(len(sel)), b.F(mean(relRows), 4), b.F(mean(sel), 4), b.CI(iv, 4)})
	}
	md.WriteString(b.Table([]string{"Targets", "Pairs", "Mean p, relevant neighbors", "Mean p, non-relevant neighbors", "Difference"}, nb))
	nr, nn := cond("neighbor-rel", "neighbor-rel"), cond("neighbor-non", "neighbor-non")
	fmt.Fprintf(&md, "\nTarget AUC: %s with relevant neighbors, %s with non-relevant neighbors.\n", b.F(b.AUC(nr.Items), 4), b.F(b.AUC(nn.Items), 4))

	// Resources.
	md.WriteString("\n## Resources\n\nMeasured request latency at 16 workers with a 1,000 requests/minute client limit; list-price cost from reported input tokens ($0.042/M, output free).\n\n")
	res := b.Resources(answers)
	arms := []string{"atomic", "retest"}
	for _, k := range packSizes {
		arms = append(arms, fmt.Sprintf("pack-%d-p1", k), fmt.Sprintf("pack-%d-p2", k))
	}
	arms = append(arms, "swap-10", "ref-index-10", "ref-id-10", "cross-4", "cross-20", "single-10", "single-50", "neighbor-rel", "neighbor-non")
	md.WriteString(b.ResourceTable(res, arms))
	if e := b.AtomicWrite(filepath.Join(out, "REPORT.md"), []byte(md.String())); e != nil {
		return e
	}
	fmt.Print(md.String())
	return b.WriteJSON(filepath.Join(out, "resources.json"), res)
}

func mean(rs []b.Row) float64 {
	if len(rs) == 0 {
		return math.NaN()
	}
	t := 0.0
	for _, r := range rs {
		t += r.P
	}
	return t / float64(len(rs))
}

func indexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
