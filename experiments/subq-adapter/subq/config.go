// Package subq lets the Jev experiments send their frozen systemone
// requests to the public-preview SubQ Snap Decisions API.
//
// The SubQ endpoint is Jev-compatible (POST /v1/systemone with model,
// state and typed questions), so no translation is needed. The frozen Jev body
// is sent with recorded edits only (see Prepare):
//
//   - "model" is set to the configured SubQ model or deployment name, and
//   - top-level members other than model, state and questions are removed
//     (none of the frozen bodies has any; the edit is recorded if one does).
//
// Receipts carry two hashes: the frozen Jev body hash (for pairing with frozen
// Jev answers) and the hash of the bytes actually sent.
//
// The public model name and list price are fixed. Rate limits are not
// documented, so concurrency remains modest by default.
package subq

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Provider is the provider name used in flags, configs and receipts.
const Provider = "subq"

// Defaults.
const (
	// DefaultURL is the public SubQ API and needs SUBQ_API_KEY.
	DefaultURL = "https://api.subq.ai/v1/systemone"
	// DirectURL names the same public endpoint for receipt compatibility.
	DirectURL = DefaultURL
	// DefaultModel is the public-preview decision model advertised by the
	// Models API and the Decisions documentation.
	DefaultModel = "subq-snap-preview"
	// Published list prices for SubQ Snap: $0.05/M input, output free.
	// Cached input has no separately published discount, so it uses the input
	// rate. Source: https://docs.subq.ai/using-the-api/models/
	DefaultUSDPerMTokInput  = 0.05
	DefaultUSDPerMTokCached = 0.05
	DefaultUSDPerMTokOutput = 0.00
	// DefaultUSDPerMTok is retained for callers that only price input.
	DefaultUSDPerMTok = DefaultUSDPerMTokInput
	// DefaultWorkers is deliberately modest: rate limits are unknown.
	DefaultWorkers = 4
	DefaultTimeout = 2 * time.Minute
	// DefaultMaxAttempts is first attempt + retries on 408/429/5xx/transport.
	DefaultMaxAttempts = 4
	// DefaultBackoff is the base retry sleep (doubled per attempt) when the
	// server sends no Retry-After header.
	DefaultBackoff = 2 * time.Second
	// MaxRetryAfter caps how long a Retry-After header can make us sleep.
	MaxRetryAfter = 2 * time.Minute
)

// Config selects the endpoint, model and accounting of a run. The zero value
// is not usable; call Resolve (or FromEnv).
type Config struct {
	URL    string `json:"endpoint"`
	APIKey string `json:"-"` // SUBQ_API_KEY; never logged or recorded
	// Model is the SubQ model name. The public-preview model is the default.
	Model string `json:"model"`
	// Rates in USD per million tokens. Input applies to uncached input
	// tokens, Cached to cached_input_tokens, Output to output_tokens.
	USDPerMTokInput  float64       `json:"usd_per_mtok_input"`
	USDPerMTokCached float64       `json:"usd_per_mtok_cached_input"`
	USDPerMTokOutput float64       `json:"usd_per_mtok_output"`
	Workers          int           `json:"workers"`
	MaxAttempts      int           `json:"max_attempts"`
	Backoff          time.Duration `json:"-"`
	Timeout          time.Duration `json:"-"`

	resolved bool
}

// Resolve fills defaults. An empty Model stays empty (see RequireModel).
func (c Config) Resolve() Config {
	if c.resolved {
		return c
	}
	if c.URL == "" {
		c.URL = DefaultURL
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.USDPerMTokInput <= 0 {
		c.USDPerMTokInput = DefaultUSDPerMTokInput
	}
	if c.USDPerMTokCached <= 0 {
		c.USDPerMTokCached = c.USDPerMTokInput
	}
	if c.USDPerMTokOutput < 0 {
		c.USDPerMTokOutput = DefaultUSDPerMTokOutput
	}
	if c.Workers <= 0 {
		c.Workers = DefaultWorkers
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = DefaultMaxAttempts
	}
	if c.Backoff <= 0 {
		c.Backoff = DefaultBackoff
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	c.resolved = true
	return c
}

// FromEnv reads the configuration:
//
//	SUBQ_API_KEY                    bearer token (never read from ~/.secrets)
//	SUBQ_MODEL                       model override (default subq-snap-preview)
//	SUBQ_URL              endpoint (default DefaultURL; point at subq-fake offline)
//	SUBQ_USD_PER_MTOK_INPUT         uncached input rate (default 0.05)
//	SUBQ_USD_PER_MTOK_CACHED_INPUT  cached input rate (default = input rate)
//	SUBQ_USD_PER_MTOK_OUTPUT        output rate (default 0.00)
//	SUBQ_WORKERS          concurrent requests (default 4)
//	SUBQ_MAX_ATTEMPTS     attempts per request (default 4)
func FromEnv() (Config, error) {
	var c Config
	c.URL = strings.TrimSpace(os.Getenv("SUBQ_URL"))
	c.APIKey = strings.TrimSpace(os.Getenv("SUBQ_API_KEY"))
	c.Model = strings.TrimSpace(os.Getenv("SUBQ_MODEL"))
	for _, f := range []struct {
		env string
		dst *float64
	}{
		{"SUBQ_USD_PER_MTOK_INPUT", &c.USDPerMTokInput},
		{"SUBQ_USD_PER_MTOK_CACHED_INPUT", &c.USDPerMTokCached},
		{"SUBQ_USD_PER_MTOK_OUTPUT", &c.USDPerMTokOutput},
	} {
		if v := strings.TrimSpace(os.Getenv(f.env)); v != "" {
			x, e := strconv.ParseFloat(v, 64)
			if e != nil || x < 0 || (f.env != "SUBQ_USD_PER_MTOK_OUTPUT" && x == 0) {
				return c, fmt.Errorf("%s must be non-negative (and input rates positive), got %q", f.env, v)
			}
			*f.dst = x
		}
	}
	for _, f := range []struct {
		env string
		dst *int
	}{
		{"SUBQ_WORKERS", &c.Workers},
		{"SUBQ_MAX_ATTEMPTS", &c.MaxAttempts},
	} {
		if v := strings.TrimSpace(os.Getenv(f.env)); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 {
				return c, fmt.Errorf("%s must be a positive integer, got %q", f.env, v)
			}
			*f.dst = n
		}
	}
	return c.Resolve(), nil
}

// RequireModel fails when no model name is configured. Every command that
// sends a request calls it, including callers that construct Config directly.
func (c Config) RequireModel() error {
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("SUBQ_MODEL is not set")
	}
	if strings.HasPrefix(strings.ToLower(c.Model), "jev-") || strings.HasPrefix(strings.ToLower(c.Model), "jev_") {
		return fmt.Errorf("SUBQ_MODEL %q looks like a Jev model name; use the name SubQ provided", c.Model)
	}
	return nil
}

// WireNotes describes what is sent, for manifests.
const WireNotes = "POST /v1/systemone, Authorization: Bearer; body = frozen Jev body with model set to the configured SubQ model " +
	"(prepare " + PrepareVersion + "); state and questions bytes and order unchanged; no reasoning_effort, no images, no options; " +
	"limits enforced offline: <=16 questions (observed server validation), <=255 choice options, 2-10 score levels, <=13 MiB request body"

// HarnessCommit is SUBQ_HARNESS_COMMIT, else `git rev-parse HEAD` of the
// working directory (with "-dirty" when the tree has changes), else "unknown".
func HarnessCommit() string {
	commitOnce.Do(func() { commit = harnessCommit() })
	return commit
}

var (
	commitOnce sync.Once
	commit     string
)

func harnessCommit() string {
	if v := strings.TrimSpace(os.Getenv("SUBQ_HARNESS_COMMIT")); v != "" {
		return v
	}
	out, e := exec.Command("git", "rev-parse", "HEAD").Output()
	if e != nil {
		return "unknown"
	}
	h := strings.TrimSpace(string(out))
	if st, e := exec.Command("git", "status", "--porcelain", "--untracked-files=no", ".").Output(); e == nil && len(strings.TrimSpace(string(st))) > 0 {
		h += "-dirty"
	}
	return h
}

// Official reports whether the endpoint is the real SubQ API, through
// the exe.dev integration or directly.
func (c Config) Official() bool { u := c.Resolve().URL; return u == DefaultURL || u == DirectURL }

// LogicalURL is the endpoint identity used by same-run checks, receipts and
// caches: the exe.dev integration URL maps to the SubQ endpoint it
// fronts (DirectURL), so a run directory started on one route resumes on the
// other. Manifests and receipts keep recording the URL actually used.
func LogicalURL(u string) string {
	if u == DefaultURL {
		return DirectURL
	}
	return u
}

// SameEndpoint reports whether two endpoint URLs are the same logical endpoint.
func SameEndpoint(a, b string) bool { return LogicalURL(a) == LogicalURL(b) }

// LogicalIdentity is Identity with the endpoint replaced by LogicalURL, for
// comparing a stored identity with the current configuration.
func LogicalIdentity(id map[string]any) map[string]any {
	out := make(map[string]any, len(id))
	for k, v := range id {
		out[k] = v
	}
	if u, ok := out["endpoint"].(string); ok {
		out["endpoint"] = LogicalURL(u)
	}
	return out
}

// NeedsKey reports the public endpoint without SUBQ_API_KEY.
func (c Config) NeedsKey() bool { return c.APIKey == "" && c.Resolve().URL == DirectURL }

// KeyMissing is the NeedsKey error text.
const KeyMissing = "SUBQ_API_KEY is not set"

// PlaceholderPrice is retained in the receipt schema for compatibility. The
// defaults are published list prices, so it is always false.
func (c Config) PlaceholderPrice() bool {
	return false
}

// Suffix is the results-directory suffix.
const Suffix = "subq-v1"

// OutDir returns results/<study>-subq-v1.
func OutDir(study string) string { return "results/" + study + "-" + Suffix }

// MatchesModel reports whether a response's model label belongs to this
// config: equal, or equal after dropping an "accounts/<a>/models/" style
// path prefix on either side (SubQ may echo the full resource name).
func (c Config) MatchesModel(label string) bool {
	m := c.Resolve().Model
	if label == m {
		return true
	}
	last := func(s string) string { return s[strings.LastIndex(s, "/")+1:] }
	return m != "" && label != "" && last(label) == last(m)
}

// Meta is the provenance recorded in manifests (never the key).
func (c Config) Meta() map[string]any {
	c = c.Resolve()
	return map[string]any{
		"provider":                  Provider,
		"endpoint":                  c.URL,
		"official_endpoint":         c.Official(),
		"model":                     c.Model,
		"usd_per_mtok_input":        c.USDPerMTokInput,
		"usd_per_mtok_cached_input": c.USDPerMTokCached,
		"usd_per_mtok_output":       c.USDPerMTokOutput,
		"usd_is_placeholder":        c.PlaceholderPrice(),
		"workers":                   c.Workers,
		"max_attempts":              c.MaxAttempts,
		"auth":                      c.APIKey != "",
		"prepare_version":           PrepareVersion,
		"wire_notes":                WireNotes,
		"harness_commit":            HarnessCommit(),
		"timeout_seconds":           c.Timeout.Seconds(),
		"cost_formula":              CostFormula,
		"preview":                   "public preview",
	}
}

// Identity is the frozen identity of a run directory: endpoint, model and
// body-preparation version. Prices, workers and retries may change between
// sessions without invalidating receipts. It records the URL actually used;
// compare identities with LogicalIdentity or SameRun, which treat the exe.dev
// integration and the direct endpoint as one.
func (c Config) Identity() map[string]any {
	c = c.Resolve()
	return map[string]any{"provider": Provider, "endpoint": c.URL, "model": c.Model, "prepare_version": PrepareVersion}
}

// SameRun reports whether two configs produce comparable receipts. The
// integration and direct URLs are the same logical endpoint.
func (c Config) SameRun(o Config) bool {
	c, o = c.Resolve(), o.Resolve()
	return SameEndpoint(c.URL, o.URL) && c.Model == o.Model
}
