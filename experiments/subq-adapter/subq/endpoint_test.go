package subq_test

import (
    "testing"

    "github.com/dorkitude/decision-model-testing/experiments/subq-adapter/subq"
)

func TestPublicEndpointNeedsKey(t *testing.T) {
    c := subq.Config{Model: "m"}.Resolve()
    if c.URL != subq.DirectURL || !c.NeedsKey() {
        t.Fatal("the public API must be the default and require SUBQ_API_KEY")
    }
    c.APIKey = "test-key"
    if c.NeedsKey() || !c.Official() || subq.LogicalURL(c.URL) != subq.DirectURL {
        t.Fatal("configured public endpoint must be official")
    }
    local := subq.Config{URL: "http://127.0.0.1:9/v1/systemone", Model: "m"}.Resolve()
    if local.NeedsKey() || local.Official() {
        t.Fatal("local fake must not require a real key or be official")
    }
}
