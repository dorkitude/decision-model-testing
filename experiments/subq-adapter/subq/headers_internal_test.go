package subq

import "testing"

func TestHeaderFilterKeepsRateLimitsDropsSecrets(t *testing.T) {
	for _, n := range []string{"x-ratelimit-remaining-tokens-generated", "x-ratelimit-limit-tokens-prompt", "X-RateLimit-Remaining-Requests"} {
		if secretHeader(n) || !keepValue(n) {
			t.Errorf("%s: rate-limit header must be kept", n)
		}
	}
	for _, n := range []string{"Authorization", "Set-Cookie", "x-api-key", "x-session-token"} {
		if !secretHeader(n) {
			t.Errorf("%s: must be treated as secret", n)
		}
	}
}

func TestServerLimit(t *testing.T) {
	limit := []byte(`{"error":{"message":"packed length 30080 exceeds limit 16384","param":null,"code":"BAD_REQUEST","type":"error"}}`)
	if !ServerLimit(400, limit) {
		t.Fatal("subq's packed-length 400 must be a server limit")
	}
	questions := []byte(`{"detail":{"error_type":"validation_error","message":"questions: must NOT have more than 16 properties"}}`)
	if !ServerLimit(422, questions) {
		t.Fatal("subq's observed 16-question validation must be a server limit")
	}
	if ServerLimit(400, []byte(`{"error":{"message":"question \"q\": unknown question type \"bool\"","code":"BAD_REQUEST"}}`)) || ServerLimit(429, limit) {
		t.Fatal("other errors are not server limits")
	}
}
