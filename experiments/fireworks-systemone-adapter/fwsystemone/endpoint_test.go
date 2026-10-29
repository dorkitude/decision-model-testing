package fwsystemone_test

import (
	"testing"

	"github.com/dorkitude/decision-model-testing/experiments/fireworks-systemone-adapter/fwsystemone"
)

// The exe.dev integration and the direct Fireworks endpoint are one logical
// endpoint: run directories started on either route resume on the other.
func TestLogicalEndpoint(t *testing.T) {
	if fwsystemone.LogicalURL(fwsystemone.DefaultURL) != fwsystemone.DirectURL || fwsystemone.LogicalURL(fwsystemone.DirectURL) != fwsystemone.DirectURL {
		t.Fatal("integration URL must map to the direct URL")
	}
	if fwsystemone.LogicalURL("http://127.0.0.1:9/inference/v1/systemone") != "http://127.0.0.1:9/inference/v1/systemone" {
		t.Fatal("other URLs must map to themselves")
	}
	a := fwsystemone.Config{URL: fwsystemone.DefaultURL, Model: "m"}
	b := fwsystemone.Config{URL: fwsystemone.DirectURL, Model: "m"}
	if !a.SameRun(b) || !b.SameRun(a) || a.SameRun(fwsystemone.Config{URL: "http://127.0.0.1:9/x", Model: "m"}) {
		t.Fatal("SameRun must treat the integration and direct URL as one endpoint")
	}
	if a.Identity()["endpoint"] != fwsystemone.DefaultURL {
		t.Fatal("Identity must record the URL actually used")
	}
	la, lb := fwsystemone.LogicalIdentity(a.Identity()), fwsystemone.LogicalIdentity(b.Identity())
	if la["endpoint"] != lb["endpoint"] || a.Identity()["endpoint"] != fwsystemone.DefaultURL {
		t.Fatal("LogicalIdentity must normalize without mutating its input")
	}
	if a.NeedsKey() || !b.NeedsKey() || (fwsystemone.Config{URL: fwsystemone.DirectURL, APIKey: "k"}).NeedsKey() {
		t.Fatal("only the direct endpoint needs FIREWORKS_API_KEY")
	}
}
