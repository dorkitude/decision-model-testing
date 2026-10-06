package bench

import (
	"math"
	"sort"
)

const BootSeed = 20260918

// NDCG10 ranks a query's candidates by score, breaking ties by BM25 rank, with
// linear gains over all official judgments (matching trec_eval ndcg_cut.10).
func NDCG10(q *Query, score map[string]float64) float64 {
	items := append([]Item(nil), q.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		a, b := score[items[i].DocID], score[items[j].DocID]
		if a != b {
			return a > b
		}
		return items[i].Rank < items[j].Rank
	})
	ideal := []int{}
	for _, g := range q.Qrels {
		ideal = append(ideal, g)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	idcg, dcg := 0.0, 0.0
	for i := 0; i < 10 && i < len(ideal); i++ {
		idcg += float64(ideal[i]) / math.Log2(float64(i+2))
	}
	for i := 0; i < 10 && i < len(items); i++ {
		if g := q.Qrels[items[i].DocID]; g > 0 {
			dcg += float64(g) / math.Log2(float64(i+2))
		}
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// Scored is one judged decision.
type Scored struct {
	Query string
	Year  string
	DocID string
	Grade int
	P     float64
}

func (s Scored) Y() float64 {
	if s.Grade >= 2 {
		return 1
	}
	return 0
}

// AUC is the Mann–Whitney probability that a relevant item outscores a non-relevant one.
func AUC(xs []Scored) float64 {
	s := append([]Scored(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i].P < s[j].P })
	var pos, neg, rankSum float64
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j].P == s[i].P {
			j++
		}
		avg := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			if s[k].Y() == 1 {
				rankSum += avg
				pos++
			} else {
				neg++
			}
		}
		i = j
	}
	if pos == 0 || neg == 0 {
		return math.NaN()
	}
	return (rankSum - pos*(pos+1)/2) / (pos * neg)
}

func Brier(xs []Scored) float64 {
	t := 0.0
	for _, x := range xs {
		t += (x.P - x.Y()) * (x.P - x.Y())
	}
	return t / float64(len(xs))
}

// ECE uses ten equal-mass bins.
func ECE(xs []Scored) float64 {
	s := append([]Scored(nil), xs...)
	sort.SliceStable(s, func(i, j int) bool { return s[i].P < s[j].P })
	n := len(s)
	t := 0.0
	for b := 0; b < 10; b++ {
		lo, hi := b*n/10, (b+1)*n/10
		if hi <= lo {
			continue
		}
		var p, y float64
		for _, x := range s[lo:hi] {
			p += x.P
			y += x.Y()
		}
		t += math.Abs(p-y) / float64(n)
	}
	return t
}

func MeanP(xs []Scored) float64 {
	t := 0.0
	for _, x := range xs {
		t += x.P
	}
	return t / float64(len(xs))
}

func Mean(a []float64) float64 {
	if len(a) == 0 {
		return math.NaN()
	}
	t := 0.0
	for _, v := range a {
		t += v
	}
	return t / float64(len(a))
}

func Percentile(a []float64, p float64) float64 {
	if len(a) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), a...)
	sort.Float64s(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return s[i]
}

// Interval is a point estimate with a 95% percentile bootstrap interval and two-sided bootstrap p.
type Interval struct {
	Est float64 `json:"estimate"`
	Lo  float64 `json:"ci95_low"`
	Hi  float64 `json:"ci95_high"`
	P   float64 `json:"p_boot"`
}

// ClusterBoot resamples queries within year and recomputes stat on the pooled sample.
// stat receives the multiset of sampled query keys.
func ClusterBoot(queries []string, year func(string) string, stat func([]string) float64, reps int, seed uint64, label string) Interval {
	strata := map[string][]string{}
	for _, q := range queries {
		strata[year(q)] = append(strata[year(q)], q)
	}
	keys := make([]string, 0, len(strata))
	for k := range strata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	r := RNG(seed, label)
	est := stat(queries)
	var draws []float64
	sample := make([]string, 0, len(queries))
	for i := 0; i < reps; i++ {
		sample = sample[:0]
		for _, k := range keys {
			s := strata[k]
			for range s {
				sample = append(sample, s[r.IntN(len(s))])
			}
		}
		if v := stat(sample); !math.IsNaN(v) {
			draws = append(draws, v)
		}
	}
	le, ge := 0, 0
	for _, d := range draws {
		if d <= 0 {
			le++
		}
		if d >= 0 {
			ge++
		}
	}
	p := 2 * math.Min(float64(le), float64(ge)) / float64(len(draws))
	return Interval{est, Percentile(draws, .025), Percentile(draws, .975), math.Min(1, p)}
}

// Holm returns Holm-adjusted p-values in input order.
func Holm(ps []float64) []float64 {
	idx := make([]int, len(ps))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return ps[idx[a]] < ps[idx[b]] })
	out := make([]float64, len(ps))
	run := 0.0
	for rank, i := range idx {
		v := math.Min(1, float64(len(ps)-rank)*ps[i])
		run = math.Max(run, v)
		out[i] = run
	}
	return out
}

// GroupByQuery indexes judged decisions for cluster resampling.
func GroupByQuery(xs []Scored) map[string][]Scored {
	g := map[string][]Scored{}
	for _, x := range xs {
		g[x.Query] = append(g[x.Query], x)
	}
	return g
}

func Pool(g map[string][]Scored, qs []string) []Scored {
	var out []Scored
	for _, q := range qs {
		out = append(out, g[q]...)
	}
	return out
}
