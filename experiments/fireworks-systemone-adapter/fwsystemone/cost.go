package fwsystemone

import (
	"sort"
	"strconv"
	"time"
)

// CostFormula states the accounting rule recorded beside every cost.
const CostFormula = "USD = ((input_tokens - cached_input_tokens) x usd_per_mtok_input + cached_input_tokens x usd_per_mtok_cached_input + output_tokens x usd_per_mtok_output) / 1e6. " +
	"Rates are PLACEHOLDERS ($1.00/Mtok each by default) until Fireworks publishes System One pricing; cached_input_tokens is assumed to be included in input_tokens."

// USDOf prices one usage object.
func (c Config) USDOf(u Usage) float64 {
	c = c.Resolve()
	cached := min(max(u.CachedInputTokens, 0), max(u.InputTokens, 0))
	return (float64(u.InputTokens-cached)*c.USDPerMTokInput +
		float64(cached)*c.USDPerMTokCached +
		float64(u.OutputTokens)*c.USDPerMTokOutput) / 1e6
}

// Span is one attempt, as recorded in a receipt.
type Span struct {
	Started   time.Time
	Seconds   float64 // measured round trip
	Questions int
	Usage     Usage
	OK        bool
	Status    int    // HTTP status, 0 for transport errors
	Code      string // error code (Fireworks error.code, TRANSPORT, PARSE, SERVICE_LIMIT)
	Attempt   int    // 1-based
	NotSent   bool   // service limit: recorded, never sent
}

// SpanOf turns a receipt into a Span.
func SpanOf(started time.Time, r Receipt, ok bool) Span {
	return Span{Started: started, Seconds: r.RoundTripSeconds, Questions: r.Questions, OK: ok, Status: r.HTTPStatus,
		Code: r.ErrorCode, Attempt: r.Attempt, NotSent: r.ServiceLimit != nil,
		Usage: Usage{InputTokens: r.InputTokens, CachedInputTokens: r.CachedInputTokens, OutputTokens: r.OutputTokens}}
}

// Cost is the run-level summary of a study: requests, outcomes, retries,
// latency, tokens, placeholder cost and observed throughput (to learn the
// real rate limits). Attempts are counted individually; failed attempts are
// priced if they report usage (normally they do not).
type Cost struct {
	Attempts          int            `json:"attempts"`
	Requests          int            `json:"requests"` // first attempts
	OKRequests        int            `json:"ok_requests"`
	FailedAttempts    int            `json:"failed_attempts"`
	FailuresByStatus  map[string]int `json:"failures_by_status"`
	FailuresByCode    map[string]int `json:"failures_by_code"`
	Retries           int            `json:"retries"` // attempts after the first
	RateLimited       int            `json:"http_429"`
	ServiceLimits     int            `json:"service_limits_not_sent"`
	Questions         int            `json:"questions_answered"`
	InputTokens       int64          `json:"input_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CachedShare       float64        `json:"cached_input_share"`
	USD               float64        `json:"usd"`
	USDPer1kRequests  float64        `json:"usd_per_1k_requests"`
	USDPer1kQuestions float64        `json:"usd_per_1k_questions"`
	USDIsPlaceholder  bool           `json:"usd_is_placeholder"`
	RateInput         float64        `json:"usd_per_mtok_input"`
	RateCached        float64        `json:"usd_per_mtok_cached_input"`
	RateOutput        float64        `json:"usd_per_mtok_output"`
	FirstStartUTC     string         `json:"first_start_utc,omitempty"`
	LastEndUTC        string         `json:"last_end_utc,omitempty"`
	SpanSeconds       float64        `json:"span_wall_seconds"`
	BusySeconds       float64        `json:"busy_wall_seconds"`
	MeanRoundTrip     float64        `json:"mean_round_trip_seconds"`
	P50RoundTrip      float64        `json:"p50_round_trip_seconds"`
	P90RoundTrip      float64        `json:"p90_round_trip_seconds"`
	P99RoundTrip      float64        `json:"p99_round_trip_seconds"`
	AttemptsPerSecond float64        `json:"attempts_per_span_second"`
	QuestionsPerSec   float64        `json:"questions_per_span_second"`
	InputTokPerMinute float64        `json:"input_tokens_per_span_minute"`
	PeakPerMinute     int            `json:"peak_attempts_started_in_one_minute"`
	Basis             string         `json:"basis"`
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(float64(len(sorted))*p))]
}

// CostOf computes the run summary of a set of attempts.
func (c Config) CostOf(spans []Span) Cost {
	c = c.Resolve()
	out := Cost{USDIsPlaceholder: c.PlaceholderPrice(), RateInput: c.USDPerMTokInput, RateCached: c.USDPerMTokCached,
		RateOutput: c.USDPerMTokOutput, Basis: CostFormula, FailuresByStatus: map[string]int{}, FailuresByCode: map[string]int{}}
	type iv struct{ a, b time.Time }
	var ivs []iv
	var rts []float64
	perMin := map[int64]int{}
	for _, s := range spans {
		if s.NotSent {
			out.ServiceLimits++
			continue
		}
		out.Attempts++
		if s.Attempt <= 1 {
			out.Requests++
		} else {
			out.Retries++
		}
		if s.Status == 429 {
			out.RateLimited++
		}
		if s.OK {
			out.OKRequests++
			out.Questions += s.Questions
		} else {
			out.FailedAttempts++
			out.FailuresByStatus[strconv.Itoa(s.Status)]++
			code := s.Code
			if code == "" {
				code = "UNKNOWN"
			}
			out.FailuresByCode[code]++
		}
		out.InputTokens += int64(s.Usage.InputTokens)
		out.CachedInputTokens += int64(s.Usage.CachedInputTokens)
		out.OutputTokens += int64(s.Usage.OutputTokens)
		out.USD += c.USDOf(s.Usage)
		rts = append(rts, s.Seconds)
		if !s.Started.IsZero() {
			ivs = append(ivs, iv{s.Started, s.Started.Add(time.Duration(s.Seconds * float64(time.Second)))})
			perMin[s.Started.Unix()/60]++
		}
	}
	for _, n := range perMin {
		out.PeakPerMinute = max(out.PeakPerMinute, n)
	}
	if out.InputTokens > 0 {
		out.CachedShare = float64(out.CachedInputTokens) / float64(out.InputTokens)
	}
	if len(rts) > 0 {
		sort.Float64s(rts)
		sum := 0.0
		for _, v := range rts {
			sum += v
		}
		out.MeanRoundTrip = sum / float64(len(rts))
		out.P50RoundTrip, out.P90RoundTrip, out.P99RoundTrip = pct(rts, 0.5), pct(rts, 0.9), pct(rts, 0.99)
	}
	if len(ivs) > 0 {
		sort.Slice(ivs, func(i, j int) bool { return ivs[i].a.Before(ivs[j].a) })
		cur := ivs[0]
		first, last := ivs[0].a, ivs[0].b
		for _, x := range ivs[1:] {
			if x.b.After(last) {
				last = x.b
			}
			if x.a.After(cur.b) {
				out.BusySeconds += cur.b.Sub(cur.a).Seconds()
				cur = x
			} else if x.b.After(cur.b) {
				cur.b = x.b
			}
		}
		out.BusySeconds += cur.b.Sub(cur.a).Seconds()
		out.SpanSeconds = last.Sub(first).Seconds()
		out.FirstStartUTC = first.UTC().Format(time.RFC3339)
		out.LastEndUTC = last.UTC().Format(time.RFC3339)
	}
	if out.SpanSeconds > 0 {
		out.AttemptsPerSecond = float64(out.Attempts) / out.SpanSeconds
		out.QuestionsPerSec = float64(out.Questions) / out.SpanSeconds
		out.InputTokPerMinute = float64(out.InputTokens) / out.SpanSeconds * 60
	}
	if out.Requests > 0 {
		out.USDPer1kRequests = out.USD / float64(out.Requests) * 1000
	}
	if out.Questions > 0 {
		out.USDPer1kQuestions = out.USD / float64(out.Questions) * 1000
	}
	return out
}

// EstimateTokens is a planning estimate of a body's input tokens: one token
// per 4 bytes of the sent body. The tokenizer is unknown; replace with the
// usage of the first real requests.
func EstimateTokens(body []byte) int { return (len(body) + 3) / 4 }

// Estimate is a planning estimate for a batch of requests.
type Estimate struct {
	Requests         int     `json:"requests"`
	Questions        int     `json:"questions"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	USD              float64 `json:"usd"`
	USDIsPlaceholder bool    `json:"usd_is_placeholder"`
	Basis            string  `json:"basis"`
}

// EstimateRun prices n requests with the given total input and output token
// estimates, no caching assumed (an upper bound if state caching applies).
func (c Config) EstimateRun(n, questions int, inputTokens, outputTokens int64) Estimate {
	c = c.Resolve()
	usd := (float64(inputTokens)*c.USDPerMTokInput + float64(outputTokens)*c.USDPerMTokOutput) / 1e6
	return Estimate{Requests: n, Questions: questions, InputTokens: inputTokens, OutputTokens: outputTokens, USD: usd,
		USDIsPlaceholder: c.PlaceholderPrice(), Basis: "input tokens ≈ sent bytes / 4, no cache discount; " + CostFormula}
}
