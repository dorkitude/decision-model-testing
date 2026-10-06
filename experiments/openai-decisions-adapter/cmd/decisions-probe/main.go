// Command decisions-probe reports whether this account can use the OpenAI
// Decisions API. The default check is free (empty body, no inference).
// --smoke additionally sends one three-question decision (noul, choice, score) (a fraction of a cent)
// and prints the raw and Jev-normalized responses, to confirm the wire format.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/dorkitude/decision-model-testing/experiments/openai-decisions-adapter/decisions"
)

func main() {
	smoke := flag.Bool("smoke", false, "send one tiny paid decision after a successful probe")
	model := flag.String("model", decisions.DefaultModel, "Decisions API model")
	url := flag.String("url", decisions.Endpoint, "endpoint")
	flag.Parse()
	tok, e := decisions.TokenFor(*url)
	if e != nil {
		fail(e)
	}
	ctx := context.Background()
	p, e := decisions.Probe(ctx, nil, *url, tok)
	if e != nil {
		fail(e)
	}
	out := map[string]any{"probe": p, "wire_version": decisions.WireVersion}
	if *smoke && p.Access == decisions.AccessOpen {
		body := []byte(`{"model":"jev-1.13.0","questions":{"noon":{"type":"noul","instructions":"Is the meeting at noon?"},"when":{"type":"choice","instructions":"When is the meeting?","criteria":{"morning":"Before noon.","noon":"At noon.","evening":"After 5pm."}},"urgency":{"type":"score","instructions":"How urgent is attending this meeting?","criteria":["low","medium","high"]}},"state":"The meeting is at noon."}`)
		r, e := decisions.Client{Token: tok, Model: *model, Endpoint: *url}.Do(ctx, body)
		if e != nil {
			fail(e)
		}
		out["smoke"] = map[string]any{"status": r.Status, "seconds": r.Seconds, "wire_request": json.RawMessage(r.WireBody), "raw": string(r.Raw), "normalized": json.RawMessage(orNull(r.Normalized)), "parse_error": r.ParseError}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	if p.Access != decisions.AccessOpen {
		os.Exit(3)
	}
	if sm, ok := out["smoke"].(map[string]any); ok && sm["normalized"].(json.RawMessage)[0] == 'n' {
		os.Exit(4) // non-2xx or unparseable: wire.go needs updating
	}
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
