package decisions

import "testing"

func TestEndpointRoutes(t *testing.T) {
	if !Official(Endpoint) || !Official(DirectEndpoint) || Official("http://127.0.0.1:9/v1/decisions") {
		t.Fatal("Official must accept the integration and the direct endpoint only")
	}
	if LogicalEndpoint(Endpoint) != DirectEndpoint || LogicalEndpoint(DirectEndpoint) != DirectEndpoint || !SameEndpoint(Endpoint, DirectEndpoint) {
		t.Fatal("the integration must be the same logical endpoint as the direct API")
	}
	for _, k := range TokenEnv {
		t.Setenv(k, "")
	}
	if tok, e := TokenFor("https://openai.int.exe.xyz/v1/decisions"); e != nil || tok != "" {
		t.Fatalf("the integration needs no token: %q %v", tok, e)
	}
	if _, e := TokenFor(""); e == nil {
		t.Fatal("the default endpoint (the OpenAI API) needs a token")
	}
	if _, e := TokenFor(DirectEndpoint); e == nil {
		t.Fatal("the direct endpoint needs a token from the environment")
	}
	t.Setenv("OPENAI_API_KEY", "env-token")
	if tok, e := TokenFor(DirectEndpoint); e != nil || tok != "env-token" {
		t.Fatal("the direct endpoint must read the token from the environment")
	}
	if IsIntegration("https://evil.example/x.int.exe.xyz") || IsIntegration("http://openai.int.exe.xyz/v1") {
		t.Fatal("IsIntegration must check the https host")
	}
}
