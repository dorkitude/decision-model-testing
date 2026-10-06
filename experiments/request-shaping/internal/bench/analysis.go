package bench

import (
	"math"
	"sort"
)

// Condition pools one or more arms (for example two pack permutations).
type Condition struct {
	Name  string
	Arms  []string
	Rows  []Row
	NDCG  map[string]float64 // per query, mean over arms; only for queries fully scored by every arm
	Items []Scored           // judged decisions pooled across arms
}

func NewCondition(d *Data, rows map[string][]Row, name string, arms ...string) *Condition {
	c := &Condition{Name: name, Arms: arms, NDCG: map[string]float64{}}
	perQuery := map[string][]float64{}
	complete := map[string]int{}
	for _, a := range arms {
		scores := map[string]map[string]float64{}
		for _, r := range rows[a] {
			c.Rows = append(c.Rows, r)
			if scores[r.D.Query] == nil {
				scores[r.D.Query] = map[string]float64{}
			}
			scores[r.D.Query][r.D.DocID] = r.P
		}
		for q, s := range scores {
			if len(s) == len(d.ByKey[q].Items) {
				perQuery[q] = append(perQuery[q], NDCG10(d.ByKey[q], s))
				complete[q]++
			}
		}
	}
	for q, v := range perQuery {
		if complete[q] == len(arms) {
			c.NDCG[q] = Mean(v)
		}
	}
	c.Items = Judged(c.Rows)
	return c
}

func (c *Condition) MeanNDCG() float64 {
	var v []float64
	for _, x := range c.NDCG {
		v = append(v, x)
	}
	return Mean(v)
}

// RefScores maps item refs to a condition's mean probability.
func (c *Condition) RefScores() map[string]float64 {
	sum := map[string]float64{}
	n := map[string]float64{}
	for _, r := range c.Rows {
		sum[r.D.Ref] += r.P
		n[r.D.Ref]++
	}
	for k := range sum {
		sum[k] /= n[k]
	}
	return sum
}

// Delta summarizes per-decision probability changes relative to a reference.
type Delta struct {
	N       int     `json:"n"`
	MeanAbs float64 `json:"mean_abs_delta"`
	Signed  float64 `json:"mean_signed_delta"`
	Changed float64 `json:"share_changed_over_0.05"`
	Flipped float64 `json:"share_crossing_0.5"`
}

func DeltaVs(rows []Row, ref map[string]float64) Delta {
	var d Delta
	for _, r := range rows {
		b, ok := ref[r.D.Ref]
		if !ok {
			continue
		}
		x := r.P - b
		d.N++
		d.MeanAbs += math.Abs(x)
		d.Signed += x
		if math.Abs(x) > 0.05 {
			d.Changed++
		}
		if (r.P >= 0.5) != (b >= 0.5) {
			d.Flipped++
		}
	}
	if d.N > 0 {
		n := float64(d.N)
		d.MeanAbs /= n
		d.Signed /= n
		d.Changed /= n
		d.Flipped /= n
	}
	return d
}

// NDCGDiff bootstraps the paired per-query nDCG@10 difference a−b.
func NDCGDiff(a, b *Condition, reps int, label string) Interval {
	var qs []string
	for q := range a.NDCG {
		if _, ok := b.NDCG[q]; ok {
			qs = append(qs, q)
		}
	}
	sort.Strings(qs)
	return ClusterBoot(qs, YearOf, func(s []string) float64 {
		t := 0.0
		for _, q := range s {
			t += a.NDCG[q] - b.NDCG[q]
		}
		return t / float64(len(s))
	}, reps, BootSeed, label)
}

// MetricDiff bootstraps a pooled item-level metric difference a−b by query cluster,
// restricted to queries present in both.
func MetricDiff(a, b []Scored, metric func([]Scored) float64, reps int, label string) Interval {
	ga, gb := GroupByQuery(a), GroupByQuery(b)
	var qs []string
	for q := range ga {
		if _, ok := gb[q]; ok {
			qs = append(qs, q)
		}
	}
	sort.Strings(qs)
	return ClusterBoot(qs, YearOf, func(s []string) float64 {
		return metric(Pool(ga, s)) - metric(Pool(gb, s))
	}, reps, BootSeed, label)
}

// Metric bootstraps a pooled item-level metric by query cluster.
func Metric(a []Scored, metric func([]Scored) float64, reps int, label string) Interval {
	g := GroupByQuery(a)
	return ClusterBoot(QueryKeys(g), YearOf, func(s []string) float64 { return metric(Pool(g, s)) }, reps, BootSeed, label)
}

// PairedRows bootstraps mean(f(row)) over rows grouped by query.
func PairedRows(rows []Row, f func(Row) (float64, bool), reps int, label string) Interval {
	g := map[string][]float64{}
	for _, r := range rows {
		if v, ok := f(r); ok {
			g[r.D.Query] = append(g[r.D.Query], v)
		}
	}
	qs := sortedKeys(g)
	return ClusterBoot(qs, YearOf, func(s []string) float64 {
		t, n := 0.0, 0.0
		for _, q := range s {
			for _, v := range g[q] {
				t += v
				n++
			}
		}
		return t / n
	}, reps, BootSeed, label)
}

// Restrict keeps judged decisions from the given queries.
func Restrict(xs []Scored, qs map[string]bool) []Scored {
	var out []Scored
	for _, x := range xs {
		if qs[x.Query] {
			out = append(out, x)
		}
	}
	return out
}

func QuerySet(qs []*Query) map[string]bool {
	m := map[string]bool{}
	for _, q := range qs {
		m[q.Key] = true
	}
	return m
}

// RandomTies returns a copy whose per-query nDCG@10 averages reps seeded random
// orderings of tied scores instead of the BM25 tie-break (a sensitivity check).
func (c *Condition) RandomTies(d *Data, reps int) *Condition {
	out := *c
	out.NDCG = map[string]float64{}
	byArm := map[string]map[string]map[string]float64{}
	for _, r := range c.Rows {
		if byArm[r.Arm] == nil {
			byArm[r.Arm] = map[string]map[string]float64{}
		}
		if byArm[r.Arm][r.D.Query] == nil {
			byArm[r.Arm][r.D.Query] = map[string]float64{}
		}
		byArm[r.Arm][r.D.Query][r.D.DocID] = r.P
	}
	for q := range c.NDCG {
		query := d.ByKey[q]
		var v []float64
		for _, a := range c.Arms {
			rng := RNG(BootSeed, "ties-"+q)
			for i := 0; i < reps; i++ {
				perm := Shuffled(query.Items, rng)
				shuffled := *query
				shuffled.Items = make([]Item, len(perm))
				for k, it := range perm {
					it.Rank = k
					shuffled.Items[k] = it
				}
				v = append(v, NDCG10(&shuffled, byArm[a][q]))
			}
		}
		out.NDCG[q] = Mean(v)
	}
	return &out
}
