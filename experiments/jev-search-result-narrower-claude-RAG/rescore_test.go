package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rescoreTransport func(*http.Request) (*http.Response, error)

func (f rescoreTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestJevRescorePayloadAndResume(t *testing.T) {
	h := testHarness(t)
	h.C.Jev = "jev-1.13.0"
	t.Setenv("TYPESAFE_TOKEN", "test-token")
	calls := 0
	p := M{"question_id": "q", "arm": "baseline", "question": "When?", "submitted_answer": "Noon.", "reference_answer": "Noon.", "source": M{"document_id": "doc", "email": "At noon."}}
	payload := jevJudgePayload(h.C.Jev, p)
	if len(obj(payload["state"])) != 4 || obj(payload["state"])["arm"] != nil || obj(obj(payload["questions"])["evaluation"])["instructions"] != judgePrompt {
		t.Fatal("changed rubric or leaked arm")
	}
	h.HTTP = &http.Client{Transport: rescoreTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"jev-1.13.0","answers":{"evaluation":{"type":"choice","choice":"correct"}},"usage":{"input_tokens":100,"output_tokens":4}}`)), Header: make(http.Header)}, nil
	})}
	if e := h.scoreSavedWithJev("q/baseline", p); e != nil {
		t.Fatal(e)
	}
	if e := h.scoreSavedWithJev("q/baseline", p); e != nil {
		t.Fatal(e)
	}
	if calls != 1 || len(h.S.all("judgments")) != 1 {
		t.Fatal("duplicate scoring")
	}
	p["submitted_answer"] = "Midnight."
	if e := h.scoreSavedWithJev("q/baseline", p); e == nil {
		t.Fatal("changed input accepted")
	}
}
func TestJevRescoreRejectsInvalidOrOversized(t *testing.T) {
	for _, response := range []string{`{"model":"other","answers":{"evaluation":{"choice":"correct"}}}`, `{"model":"jev-1.13.0","answers":{"evaluation":{"choice":"maybe"}}}`} {
		h := testHarness(t)
		h.C.Jev = "jev-1.13.0"
		t.Setenv("TYPESAFE_TOKEN", "test-token")
		h.HTTP = &http.Client{Transport: rescoreTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
		})}
		if e := h.scoreSavedWithJev("q/baseline", M{}); e == nil {
			t.Fatal("accepted invalid receipt")
		}
		if len(h.S.all("judgments")) != 0 {
			t.Fatal("invalid verdict persisted")
		}
	}
	h := testHarness(t)
	h.C.JevPageBytes = 1
	h.HTTP = &http.Client{Transport: rescoreTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("oversized input sent")
		return nil, fmt.Errorf("unexpected request")
	})}
	if e := h.scoreSavedWithJev("q/baseline", M{}); e == nil {
		t.Fatal("oversized input accepted")
	}
}
func TestRescoreRequiresCompleteConsistentSavedInputs(t *testing.T) {
	h := testHarness(t)
	for _, arm := range []string{"baseline", "jev_filtered"} {
		id := "q/" + arm
		for name, row := range map[string]M{"answers": {"id": id, "question_id": "q", "arm": arm, "answer": "Noon."}, "judge_inputs": {"id": id, "question_id": "q", "arm": arm, "submitted_answer": "Noon."}, "judgments": {"id": id + "/deepseek", "judge": "deepseek", "correct": true}} {
			if e := h.S.put(name, row); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := h.S.put("manifest", M{"id": "protocol", "config": M{"questions": 1}}); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := rescoreInputs(h.S.Dir); e != nil {
		t.Fatal(e)
	}
	if e := h.S.put("answers", M{"id": "extra", "answer": "extra"}); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := rescoreInputs(h.S.Dir); e == nil {
		t.Fatal("incomplete pairing accepted")
	}
}
