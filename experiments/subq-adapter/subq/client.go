package subq

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Result is one HTTP attempt against System One.
type Result struct {
	Started     time.Time // when the request was sent (UTC)
	Seconds     float64   // measured HTTP round trip (send to last body byte)
	TTFBSeconds float64   // send to first response byte (0 if none)
	Status      int       // 0 on transport error
	Raw         []byte    // response body as received (the full raw response)
	FrozenSHA   string    // SHA-256 of the frozen Jev body (pairs with Jev receipts)
	Sent        []byte    // bytes actually sent
	SentSHA     string    // SHA-256 of Sent
	Edits       []Edit    // what Prepare changed
	Questions   int       // questions in the body
	StateBytes  int       // bytes of the state member as sent
	// Header telemetry. HeaderNames lists every response header name except
	// auth and cookie headers; HeaderValues keeps only the values of request
	// id, SubQ timing, Retry-After and rate-limit headers.
	HeaderNames   []string
	HeaderValues  map[string]string
	HeaderReqID   string // x-request-id header
	BodyRequestID string // request_id member of an error body
	RequestID     string // HeaderReqID, else BodyRequestID
	ServerTime    string // subq-server-processing-time header, verbatim
	RetryAfter    string // Retry-After header, verbatim
	TransportErr  string // transport error text (status 0)
	Response      Response
	ParseError    string // a 2xx body that fails ParseResponse (permanent)
	Fake          bool   // the response came from subq-fake
}

// OK reports a 2xx response that passed ParseResponse and the model check.
func (r Result) OK() bool { return r.Status >= 200 && r.Status < 300 && r.ParseError == "" }

// Usage returns the reported usage (zero when absent).
func (r Result) Usage() Usage {
	if r.Response.Usage == nil {
		return Usage{}
	}
	return *r.Response.Usage
}

// ServerSeconds parses subq-server-processing-time (seconds; 0 if absent
// or not a number). The raw header is kept in the receipt.
func (r Result) ServerSeconds() float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(r.ServerTime, "s")), 64)
	return f
}

// AttemptRecord is one entry of a request's retry history.
type AttemptRecord struct {
	Attempt        int     `json:"attempt"`
	Status         int     `json:"status"`
	Code           string  `json:"code,omitempty"`
	RetryAfter     string  `json:"retry_after,omitempty"`
	BackoffSeconds float64 `json:"backoff_seconds"` // sleep after this attempt (0 for the last)
	RequestID      string  `json:"server_request_id,omitempty"`
	Seconds        float64 `json:"round_trip_seconds"`
}

// Record summarizes an attempt for a retry history.
func (r Result) Record(attempt int, backoff time.Duration) AttemptRecord {
	code := ErrorBody(r.Raw).Error.Code
	if r.Status == 0 {
		code = "TRANSPORT"
	}
	return AttemptRecord{Attempt: attempt, Status: r.Status, Code: code, RetryAfter: r.RetryAfter,
		BackoffSeconds: backoff.Seconds(), RequestID: r.RequestID, Seconds: r.Seconds}
}

// Receipt is the provenance a System One attempt adds to an experiment's
// receipt. Experiments embed these fields; payload_sha256 in the host receipt
// stays the frozen Jev body hash, and the host stores Raw verbatim as its
// response. The API key and header values other than those listed in
// Result.HeaderValues are never recorded.
type Receipt struct {
	Provider          string            `json:"provider"`
	Endpoint          string            `json:"endpoint"`
	ProviderModel     string            `json:"provider_model"`
	ResponseModel     string            `json:"response_model,omitempty"`
	PrepareVersion    string            `json:"prepare_version"`
	Edits             []Edit            `json:"prepare_edits,omitempty"`
	FrozenSHA         string            `json:"frozen_request_sha256"`
	SentSHA           string            `json:"sent_request_sha256"`
	SentBytes         int               `json:"sent_request_bytes"`
	Questions         int               `json:"question_count"`
	StateBytes        int               `json:"state_bytes"`
	Attempt           int               `json:"attempt"`
	History           []AttemptRecord   `json:"retry_history,omitempty"`
	HTTPStatus        int               `json:"http_status"`
	ErrorCode         string            `json:"error_code,omitempty"`
	ErrorMessage      string            `json:"error_message,omitempty"`
	ErrorParam        any               `json:"error_param,omitempty"`
	ServiceLimit      *LimitError       `json:"service_limit,omitempty"`
	RoundTripSeconds  float64           `json:"round_trip_seconds"`
	TTFBSeconds       float64           `json:"ttfb_seconds,omitempty"`
	ServerTime        string            `json:"server_processing_time,omitempty"`
	InputTokens       int               `json:"input_tokens"`
	CachedInputTokens int               `json:"cached_input_tokens"`
	OutputTokens      int               `json:"output_tokens"`
	UsagePresent      bool              `json:"usage_present"`
	USD               float64           `json:"usd"`
	USDIsPlaceholder  bool              `json:"usd_is_placeholder"`
	RequestID         string            `json:"server_request_id,omitempty"`
	HeaderRequestID   string            `json:"header_request_id,omitempty"`
	BodyRequestID     string            `json:"body_request_id,omitempty"`
	RetryAfter        string            `json:"retry_after,omitempty"`
	HeaderNames       []string          `json:"response_header_names,omitempty"`
	HeaderValues      map[string]string `json:"response_header_values,omitempty"`
}

// Receipt builds the provenance fields for an attempt. attempt is 1-based;
// history holds the earlier attempts of the same request (may be nil).
func (c Config) Receipt(r Result, attempt int, history []AttemptRecord) Receipt {
	c = c.Resolve()
	u := r.Usage()
	eb := ErrorBody(r.Raw)
	rc := Receipt{
		Provider: Provider, Endpoint: c.URL, ProviderModel: c.Model, ResponseModel: r.Response.Model,
		PrepareVersion: PrepareVersion, Edits: r.Edits,
		FrozenSHA: r.FrozenSHA, SentSHA: r.SentSHA, SentBytes: len(r.Sent), Questions: r.Questions, StateBytes: r.StateBytes,
		Attempt: attempt, History: history, HTTPStatus: r.Status,
		ErrorCode: eb.Error.Code, ErrorMessage: eb.Error.Message, ErrorParam: eb.Error.Param,
		RoundTripSeconds: r.Seconds, TTFBSeconds: r.TTFBSeconds, ServerTime: r.ServerTime,
		InputTokens: u.InputTokens, CachedInputTokens: u.CachedInputTokens, OutputTokens: u.OutputTokens,
		UsagePresent: r.Response.Usage != nil, USD: c.USDOf(u), USDIsPlaceholder: c.PlaceholderPrice(),
		RequestID: r.RequestID, HeaderRequestID: r.HeaderReqID, BodyRequestID: r.BodyRequestID, RetryAfter: r.RetryAfter,
		HeaderNames: r.HeaderNames, HeaderValues: r.HeaderValues,
	}
	switch {
	case r.Status == 0 && r.TransportErr != "":
		rc.ErrorCode, rc.ErrorMessage = "TRANSPORT", r.TransportErr
	case r.ParseError != "":
		rc.ErrorCode, rc.ErrorMessage = "PARSE", r.ParseError
	}
	return rc
}

// LimitReceipt is the receipt of a body that was not sent because it exceeds
// a documented limit.
func (c Config) LimitReceipt(frozen []byte, err error) Receipt {
	c = c.Resolve()
	q, sb := Counts(frozen)
	return Receipt{Provider: Provider, Endpoint: c.URL, ProviderModel: c.Model, PrepareVersion: PrepareVersion,
		FrozenSHA: Digest(frozen), Questions: q, StateBytes: sb, ErrorCode: "SERVICE_LIMIT", ErrorMessage: err.Error(),
		ServiceLimit: LimitOf(err), USDIsPlaceholder: c.PlaceholderPrice()}
}

// Client sends frozen Jev bodies to System One.
type Client struct {
	HTTP   *http.Client // nil: a client with Config.Timeout
	Config Config
}

// Do prepares and sends one frozen Jev body (one attempt; see DoRetry). A
// non-nil error means the request was not answered: a preparation failure
// (IsServiceLimit for documented limits; nothing was sent) or a transport
// error. HTTP errors and unparseable 2xx bodies are reported in Result.
func (c Client) Do(ctx context.Context, frozen []byte) (Result, error) {
	cfg := c.Config.Resolve()
	r := Result{FrozenSHA: Digest(frozen)}
	r.Questions, r.StateBytes = Counts(frozen)
	sent, edits, e := cfg.PrepareEdits(frozen)
	if e != nil {
		return r, e
	}
	r.Sent, r.SentSHA, r.Edits = sent, Digest(sent), edits
	if e := c.post(ctx, &r); e != nil {
		return r, e
	}
	if r.Status >= 200 && r.Status < 300 {
		p, e := ParseResponse(frozen, r.Raw)
		r.Response = p
		if e != nil {
			r.ParseError = e.Error()
		} else if !cfg.MatchesModel(p.Model) {
			r.ParseError = "unexpected model " + p.Model
		}
	}
	return r, nil
}

// DoRetry sends one frozen body with the retry policy: up to MaxAttempts
// attempts, retrying only transport errors, 408, 429 and 5xx, sleeping
// Retry-After when given and exponential backoff otherwise. onAttempt (may be
// nil) sees every attempt with its 1-based number and the history before it,
// so callers can write one receipt per attempt. It returns the last attempt
// and the full history. A preparation error (including a service limit)
// returns immediately with no attempt.
func (c Client) DoRetry(ctx context.Context, frozen []byte, onAttempt func(r Result, attempt int, history []AttemptRecord)) (Result, []AttemptRecord, error) {
	cfg := c.Config.Resolve()
	var hist []AttemptRecord
	var last Result
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		r, e := c.Do(ctx, frozen)
		if e != nil && r.Sent == nil {
			return r, hist, e // not sent
		}
		if e != nil {
			r.TransportErr = e.Error()
		}
		if onAttempt != nil {
			onAttempt(r, attempt, append([]AttemptRecord{}, hist...))
		}
		last = r
		if !Transient(r.Status) || attempt == cfg.MaxAttempts {
			hist = append(hist, r.Record(attempt, 0))
			break
		}
		d := cfg.Sleep(attempt, r.RetryAfter)
		hist = append(hist, r.Record(attempt, d))
		select {
		case <-ctx.Done():
			return last, hist, ctx.Err()
		case <-time.After(d):
		}
	}
	return last, hist, nil
}

// DoRaw sends body exactly as given (no Prepare, no response checks). The
// probe uses it for the free validation check.
func (c Client) DoRaw(ctx context.Context, body []byte) (Result, error) {
	r := Result{Sent: body, SentSHA: Digest(body)}
	r.Questions, r.StateBytes = Counts(body)
	e := c.post(ctx, &r)
	if e != nil {
		r.TransportErr = e.Error()
	}
	return r, e
}

// keepValue reports response headers whose values are recorded.
func keepValue(name string) bool {
	n := strings.ToLower(name)
	return n == "x-request-id" || n == "retry-after" || strings.HasPrefix(n, "subq-") ||
		strings.Contains(n, "ratelimit") || strings.Contains(n, "rate-limit")
}

func secretHeader(name string) bool {
	n := strings.ToLower(name)
	if strings.Contains(n, "ratelimit") || strings.Contains(n, "rate-limit") {
		return false // e.g. x-ratelimit-remaining-tokens-generated: rate telemetry, not a credential
	}
	return strings.Contains(n, "auth") || strings.Contains(n, "cookie") || strings.Contains(n, "api-key") || strings.Contains(n, "token")
}

func (c Client) post(ctx context.Context, r *Result) error {
	cfg := c.Config.Resolve()
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: cfg.Timeout}
	}
	var first time.Time
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }})
	req, e := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(r.Sent))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	start := time.Now()
	r.Started = start.UTC()
	resp, e := h.Do(req)
	if e != nil {
		r.Seconds = time.Since(start).Seconds()
		return e
	}
	defer resp.Body.Close()
	r.Raw, e = io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	r.Seconds = time.Since(start).Seconds()
	if !first.IsZero() {
		r.TTFBSeconds = first.Sub(start).Seconds()
	}
	r.Status = resp.StatusCode
	r.HeaderValues = map[string]string{}
	for k, v := range resp.Header {
		if secretHeader(k) {
			continue
		}
		r.HeaderNames = append(r.HeaderNames, strings.ToLower(k))
		if keepValue(k) {
			r.HeaderValues[strings.ToLower(k)] = strings.Join(v, ", ")
		}
	}
	sort.Strings(r.HeaderNames)
	r.HeaderReqID = resp.Header.Get("x-request-id")
	r.BodyRequestID = ErrorBody(r.Raw).RequestID
	r.RequestID = r.HeaderReqID
	if r.RequestID == "" {
		r.RequestID = r.BodyRequestID
	}
	r.ServerTime = resp.Header.Get("subq-server-processing-time")
	r.RetryAfter = resp.Header.Get("Retry-After")
	r.Fake = resp.Header.Get(FakeHeader) != ""
	return e
}

// Transient reports whether an attempt should be retried: transport errors
// (status 0), 408, 429 and 5xx (503 capacity, 502 upstream, 504 over the
// 5-minute limit). 400, 401, 403, 404, 413 and 422 are permanent.
func Transient(status int) bool {
	return status == 0 || status == 408 || status == 429 || status >= 500
}

// NotAllowlisted reports a missing model or a key without access to it.
// Permanent; it stops a run.
func NotAllowlisted(status int, raw []byte) bool {
	if status != 403 && status != 404 && status != 422 {
		return false
	}
	b := ErrorBody(raw)
	code := strings.ToLower(b.Error.Code)
	msg := strings.ToLower(b.Error.Message)
	return code == "access_denied" || code == "not_found" ||
		strings.Contains(msg, "model") && (strings.Contains(msg, "not found") || strings.Contains(msg, "cannot use"))
}

// ServerLimit reports the server rejecting a body over an additional,
// undocumented limit. It is permanent: record it as a service limit and
// never retry it.
func ServerLimit(status int, raw []byte) bool {
	msg := strings.ToLower(ErrorBody(raw).Error.Message)
	return status == 400 && strings.Contains(msg, "exceeds limit") ||
		status == 422 && strings.Contains(msg, "questions") && strings.Contains(msg, "more than 16 properties")
}

// Sleep returns how long to wait before retry number attempt (1-based):
// Retry-After when the server sent one (capped at MaxRetryAfter), otherwise
// base × 2^(attempt-1).
func (c Config) Sleep(attempt int, retryAfter string) time.Duration {
	c = c.Resolve()
	if s, e := strconv.ParseFloat(strings.TrimSpace(retryAfter), 64); e == nil && s >= 0 {
		return min(time.Duration(s*float64(time.Second)), MaxRetryAfter)
	}
	if t, e := http.ParseTime(retryAfter); e == nil {
		return max(0, min(time.Until(t), MaxRetryAfter))
	}
	return c.Backoff << max(0, attempt-1)
}

// SubQError accepts the Decisions API's documented detail envelope and the
// OpenAI-shaped envelope used by some other SubQ endpoints.
type SubQError struct {
	Error struct {
		Message string `json:"message"`
		Param   any    `json:"param"`
		Code    string `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
	Detail struct {
		ErrorType string `json:"error_type"`
		Message   string `json:"message"`
	} `json:"detail"`
	RequestID string `json:"request_id"`
}

// ErrorBody parses a SubQ error body (zero value if it is not one).
func ErrorBody(raw []byte) SubQError {
	var b SubQError
	json.Unmarshal(raw, &b)
	if b.Error.Message == "" && b.Detail.Message != "" {
		b.Error.Message = b.Detail.Message
		b.Error.Code = b.Detail.ErrorType
		b.Error.Type = b.Detail.ErrorType
	}
	return b
}

// ErrorDetail is "CODE: message" from an error body, or "".
func ErrorDetail(raw []byte) string {
	b := ErrorBody(raw)
	if b.Error.Message == "" {
		return ""
	}
	if b.Error.Code != "" {
		return b.Error.Code + ": " + b.Error.Message
	}
	return b.Error.Message
}
