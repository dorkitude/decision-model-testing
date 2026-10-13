// Command subq-probe checks the SubQ Snap decisions API
// (public preview). Configuration comes from the environment (see
// subq.FromEnv); the documented model is the default.
//
// Modes:
//
//	subq-probe                 one minimal request with the configured model:
//	                                  404 = not allowlisted (free); 2xx = open (one tiny request)
//	subq-probe --smoke         also one paid three-question request (noul, choice,
//	                                  score) whose response must normalize
//	subq-probe --validate D... the free 422-vs-404 check: sends a bounded sample of
//	                                  bodies (*.json under D) with a nonexistent model, so
//	                                  the server validates them and never runs inference
//
// Exit codes: 0 ok/open (or validation all valid), 2 bad configuration,
// 3 unreachable/rejected/auth failure, 4 smoke failed, 5 --require-real and
// the server is the fake, 6 not allowlisted, 7 validation found invalid or
// unexpected bodies.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/subq-adapter/subq"
)

func main() {
	c, e := subq.FromEnv()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	smoke := flag.Bool("smoke", false, "after the probe, send one paid three-question request that must normalize")
	validate := flag.Bool("validate", false, "free validation of the bodies under the given files/directories (args)")
	maxN := flag.Int("max", 8, "--validate: at most this many requests (hard cap 40)")
	pause := flag.Duration("pause", 300*time.Millisecond, "--validate: pause between requests")
	requireReal := flag.Bool("require-real", false, "fail (exit 5) if the server is subq-fake")
	out := flag.String("out", "", "also write the JSON result to this file")
	flag.Parse()
	write := func(v any) {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
		if *out != "" {
			if e := os.WriteFile(*out, append(b, '\n'), 0o644); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(2)
			}
		}
	}
	if *validate {
		if flag.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "--validate needs files or directories of request bodies")
			os.Exit(2)
		}
		if c.NeedsKey() {
			fmt.Fprintln(os.Stderr, subq.KeyMissing)
			os.Exit(2)
		}
		n := min(max(*maxN, 1), 40)
		samples, e := subq.SelectSamples(flag.Args(), n)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		checks := subq.ValidateLive(context.Background(), nil, c, samples, *pause)
		bad := 0
		for _, x := range checks {
			if x.Verdict != "valid" {
				bad++
			}
		}
		write(map[string]any{"checked_utc": time.Now().UTC().Format(time.RFC3339), "preview": "public preview",
			"endpoint": c.URL, "validation_model": subq.ValidationModel, "prepare_version": subq.PrepareVersion,
			"requests": len(checks), "invalid_or_unexpected": bad, "checks": checks})
		if bad > 0 {
			os.Exit(7)
		}
		return
	}
	r := subq.Probe(context.Background(), nil, c, *smoke)
	write(r)
	switch r.Status {
	case subq.StatusConfigError:
		os.Exit(2)
	case subq.StatusUnreachable, subq.StatusRejected, subq.StatusAuthFailed:
		os.Exit(3)
	case subq.StatusSmokeFailed:
		os.Exit(4)
	case subq.StatusNotAllowlisted:
		os.Exit(6)
	}
	if *requireReal && r.Fake {
		os.Exit(5)
	}
}
