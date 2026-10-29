// Package fwsystemone lets the Jev experiments send their frozen systemone
// requests to the Fireworks System One decisions API (public, do not
// publish).
//
// The Fireworks endpoint is Jev-compatible (POST /v1/systemone with model,
// state and typed questions), so no translation is needed. The frozen Jev body
// is sent with recorded edits only (see Prepare):
//
//   - "model" is set to the configured Fireworks model or deployment name, and
//   - top-level members other than model, state and questions are removed
//     (none of the frozen bodies has any; the edit is recorded if one does).
//
// Receipts carry two hashes: the frozen Jev body hash (for pairing with frozen
// Jev answers) and the hash of the bytes actually sent.
//
// FIREWORKS_SYSTEMONE_MODEL selects the model; this study used
// accounts/fireworks/routers/rune. Set explicit accounting rates for reports.
package fwsystemone

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
const Provider = "fireworks-systemone"

// Defaults.
const (
	// DefaultURL is the exe.dev `fireworks` integration: exe.dev injects the
	// Fireworks key at the network edge, so the VM never holds it.
	DefaultURL = "https://fireworks.int.exe.xyz/inference/v1/systemone"
	// DirectURL is the Fireworks API itself, for machines without the
	// integration. Only this endpoint needs FIREWORKS_API_KEY.
	DirectURL = "https://api.fireworks.ai/inference/v1/systemone"
	// DefaultUSDPerMTok is a PLACEHOLDER, not a price: reports must use an explicit
	// dated rate. It matches the placeholder the OpenAI
	// Decisions ports use, so the two are comparable until real rates exist.
	// Set FIREWORKS_USD_PER_MTOK_INPUT, _CACHED_INPUT and _OUTPUT.
	DefaultUSDPerMTok = 1.00
	// DefaultWorkers is deliberately modest: rate limits are unknown.
	DefaultWorkers = 4
	// DefaultTimeout is just over the server's 5-minute request cap (a
	// request over 5 minutes gets 504 from the server first).
	DefaultTimeout = 5*time.Minute + 15*time.Second
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
	APIKey string `json:"-"` // FIREWORKS_API_KEY; never logged or recorded
	// Model is the model or deployment name Fireworks gives on allowlisting.
	// This study uses accounts/fireworks/routers/rune; required for real runs.
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
	if c.USDPerMTokInput <= 0 {
		c.USDPerMTokInput = DefaultUSDPerMTok
	}
	if c.USDPerMTokCached <= 0 {
		c.USDPerMTokCached = c.USDPerMTokInput // no cache discount assumed
	}
	if c.USDPerMTokOutput <= 0 {
		c.USDPerMTokOutput = c.USDPerMTokInput
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
//	FIREWORKS_API_KEY                    bearer token, only for DirectURL (never read from ~/.secrets)
//	FIREWORKS_SYSTEMONE_MODEL            model or deployment name (required for real runs)
//	FIREWORKS_SYSTEMONE_URL              endpoint (default DefaultURL; point at fwsystemone-fake offline)
//	FIREWORKS_USD_PER_MTOK_INPUT         uncached input rate, placeholder default 1.00
//	FIREWORKS_USD_PER_MTOK_CACHED_INPUT  cached input rate (placeholder default = input rate)
//	FIREWORKS_USD_PER_MTOK_OUTPUT        output rate (placeholder default = input rate)
//	FIREWORKS_SYSTEMONE_WORKERS          concurrent requests (default 4)
//	FIREWORKS_SYSTEMONE_MAX_ATTEMPTS     attempts per request (default 4)
func FromEnv() (Config, error) {
	var c Config
	c.URL = strings.TrimSpace(os.Getenv("FIREWORKS_SYSTEMONE_URL"))
	c.APIKey = strings.TrimSpace(os.Getenv("FIREWORKS_API_KEY"))
	c.Model = strings.TrimSpace(os.Getenv("FIREWORKS_SYSTEMONE_MODEL"))
	for _, f := range []struct {
		env string
		dst *float64
	}{
		{"FIREWORKS_USD_PER_MTOK_INPUT", &c.USDPerMTokInput},
		{"FIREWORKS_USD_PER_MTOK_CACHED_INPUT", &c.USDPerMTokCached},
		{"FIREWORKS_USD_PER_MTOK_OUTPUT", &c.USDPerMTokOutput},
	} {
		if v := strings.TrimSpace(os.Getenv(f.env)); v != "" {
			x, e := strconv.ParseFloat(v, 64)
			if e != nil || x <= 0 {
				return c, fmt.Errorf("%s must be a positive number, got %q", f.env, v)
			}
			*f.dst = x
		}
	}
	for _, f := range []struct {
		env string
		dst *int
	}{
		{"FIREWORKS_SYSTEMONE_WORKERS", &c.Workers},
		{"FIREWORKS_SYSTEMONE_MAX_ATTEMPTS", &c.MaxAttempts},
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
// sends a request calls it: there is no safe default model name.
func (c Config) RequireModel() error {
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("FIREWORKS_SYSTEMONE_MODEL is not set: Fireworks provides the model or deployment name when the account is allowlisted for the System One API")
	}
	if strings.HasPrefix(strings.ToLower(c.Model), "jev-") || strings.HasPrefix(strings.ToLower(c.Model), "jev_") {
		return fmt.Errorf("FIREWORKS_SYSTEMONE_MODEL %q looks like a Jev model name; use the name Fireworks provided", c.Model)
	}
	return nil
}

// WireNotes describes what is sent, for manifests.
const WireNotes = "POST /inference/v1/systemone, Authorization: Bearer; body = frozen Jev body with model set to the Fireworks-provided name " +
	"(prepare " + PrepareVersion + "); state and questions bytes and order unchanged; no reasoning_effort, no images, no options; " +
	"limits enforced offline: <=256 questions, <=208 choice options, 2-10 score levels, <=4 MiB state, <=8 MiB body"

// HarnessCommit is FIREWORKS_HARNESS_COMMIT, else `git rev-parse HEAD` of the
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
	if v := strings.TrimSpace(os.Getenv("FIREWORKS_HARNESS_COMMIT")); v != "" {
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

// Official reports whether the endpoint is the real Fireworks API, through
// the exe.dev integration or directly.
func (c Config) Official() bool { u := c.Resolve().URL; return u == DefaultURL || u == DirectURL }

// LogicalURL is the endpoint identity used by same-run checks, receipts and
// caches: the exe.dev integration URL maps to the Fireworks endpoint it
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

// NeedsKey reports a direct endpoint without FIREWORKS_API_KEY. The exe.dev
// integration needs no key on the VM.
func (c Config) NeedsKey() bool { return c.APIKey == "" && c.Resolve().URL == DirectURL }

// KeyMissing is the NeedsKey error text.
const KeyMissing = "FIREWORKS_API_KEY is not set (only the direct endpoint needs it; on exe.dev use the default fireworks integration)"

// PlaceholderPrice reports whether any rate is still the default placeholder.
func (c Config) PlaceholderPrice() bool {
	c = c.Resolve()
	return c.USDPerMTokInput == DefaultUSDPerMTok || c.USDPerMTokCached == DefaultUSDPerMTok || c.USDPerMTokOutput == DefaultUSDPerMTok
}

// Suffix is the results-directory suffix.
const Suffix = "fireworks-systemone-v1"

// OutDir returns results/<study>-fireworks-systemone-v1.
func OutDir(study string) string { return "results/" + study + "-" + Suffix }

// MatchesModel reports whether a response's model label belongs to this
// config: equal, or equal after dropping an "accounts/<a>/models/" style
// path prefix on either side (Fireworks may echo the full resource name).
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
		"preview":                   "public",
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
