package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeResponsesAndUsage(t *testing.T) {
	raw := M{"subtype": "success", "is_error": false, "modelUsage": M{"claude-opus-5": M{}}, "result": `{"answer":"Noon [synthetic]."}`, "usage": M{"input_tokens": float64(10), "cache_read_input_tokens": float64(20), "cache_creation_input_tokens": float64(30), "output_tokens": float64(5)}}
	r, e := claudeResponse(raw, "none", "claude-opus-5")
	if e != nil {
		t.Fatal(e)
	}
	if num(obj(r["usage"])["input_tokens"]) != 60 {
		t.Fatal("cache tokens omitted")
	}
	raw["structured_output"] = M{"answer": "Structured answer."}
	if _, e = claudeResponse(raw, "none", "claude-opus-5"); e != nil {
		t.Fatal(e)
	}
	delete(raw, "structured_output")
	raw["result"] = `{"query":"meeting"}`
	r, e = claudeResponse(raw, "auto", "claude-opus-5")
	if e != nil || r["choices"].([]M)[0]["finish_reason"] != "tool_calls" {
		t.Fatal(r, e)
	}
	if _, e = claudeResponse(raw, "none", "claude-opus-5"); e == nil {
		t.Fatal("allowed extra search")
	}
	if _, e = claudeResponse(raw, "auto", "wrong-model"); e == nil {
		t.Fatal("accepted fallback model")
	}
	for _, bad := range []string{`{"answer":"","query":"x"}`, `{"answer":""}`, "not json", `{"answer":"x","extra":1}`} {
		raw["result"] = bad
		if _, e = claudeResponse(raw, "auto", "claude-opus-5"); e == nil {
			t.Fatal("accepted", bad)
		}
	}
}
func TestClaudeEnvironmentExcludesAlternateBilling(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_OAUTH_TOKEN"} {
		t.Setenv(k, "test-value")
	}
	for _, v := range claudeEnv() {
		if strings.HasSuffix(v, "=test-value") {
			t.Fatal("credential route override leaked")
		}
	}
}
func TestDisjointDeterministicSample(t *testing.T) {
	rows := []M{}
	for _, id := range []string{"a", "b", "c", "d"} {
		rows = append(rows, M{"question_id": id})
	}
	a, e := selectSample(rows, map[string]bool{"a": true}, 3, 123)
	if e != nil {
		t.Fatal(e)
	}
	b, e := selectSample([]M{rows[3], rows[2], rows[1], rows[0]}, map[string]bool{"a": true}, 3, 123)
	if e != nil || !same(a, b) {
		t.Fatal("sample depends on input order")
	}
	for _, r := range a {
		if r["question_id"] == "a" {
			t.Fatal("overlap")
		}
	}
	if _, e = selectSample(rows, map[string]bool{"a": true}, 4, 123); e == nil {
		t.Fatal("accepted undersized sample")
	}
}
func TestClaudeResumeDoesNotRunTwice(t *testing.T) {
	h := testHarness(t)
	h.C.QA = "claude-opus-5"
	h.C.Timeout = 10
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	response := M{"subtype": "success", "is_error": false, "modelUsage": M{"claude-opus-5": M{}}, "result": `{"answer":"Noon."}`, "usage": M{"input_tokens": 10, "output_tokens": 3}}
	response["type"] = "result"
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + string(canon(M{"type": "assistant", "message": M{"model": "claude-opus-5"}})) + "\n" + string(canon(response)) + "'\n"
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	h.C.ClaudeCommand = path
	payload := M{"messages": []M{{"role": "user", "content": "When?"}}, "tool_choice": "none"}
	r, e := h.claudeCall("answer/test/baseline/turn-0", "answer", payload)
	if e != nil {
		t.Fatal(e)
	}
	receipt := h.S.all("requests")[0]
	if hash(canon(M{"url": receipt["url"], "payload": receipt["payload"]})) != receipt["payload_sha256"] {
		t.Fatal("serialized request identity changed")
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	cached, e := h.claudeCall("answer/test/baseline/turn-0", "answer", payload)
	if e != nil {
		t.Fatal(e)
	}
	var normalized M
	_ = json.Unmarshal(canon(r), &normalized)
	if !same(normalized, cached) || len(h.S.all("requests")) != 1 {
		t.Fatal("resume mismatch")
	}
}
