package eval

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nativeConfig() M {
	return M{"timeout_s": 2, "max_requests": 20, "max_tokens_floor": 100, "native_rubric": "Choose the response that follows the instruction.", "atomic_criteria": M{"adherence": "Follows instructions?", "correctness": "Correct?", "relevance": "Relevant?"}, "atomic_weights": M{"adherence": 4, "correctness": 1, "relevance": 1}}
}
func TestNativeInterfacesAreBlindedAndOrderNormalized(t *testing.T) {
	t.Setenv("TYPESAFE_TOKEN", "test-only")
	t.Setenv("FIREWORKS_API_KEY", "test-only")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		if strings.Contains(string(raw), "secret-gold-marker") {
			t.Error("gold leaked")
		}
		payload := obj(decode(raw))
		jev := strings.HasPrefix(str(payload["model"]), "jev")
		state, questions := obj(payload["state"]), obj(payload["questions"])
		if !jev {
			content := str(obj(arr(payload["messages"])[0])["content"])
			index := strings.Index(content, "\n{")
			if index < 0 {
				t.Fatal("missing structured task")
			}
			task := obj(decode([]byte(content[index+1:])))
			state, questions = obj(task["state"]), obj(task["questions"])
			if obj(payload["response_format"])["type"] != "json_schema" {
				t.Error("LLM schema not enforced")
			}
		}
		answers := M{}
		for name, q := range questions {
			if obj(q)["type"] == "choice" {
				choice := "B"
				if obj(state["responses"])["A"] == "good" {
					choice = "A"
				}
				if jev {
					answers[name] = M{"choice": choice, "probabilities": M{"A": .5, "B": .5}}
				} else {
					answers[name] = choice
				}
			} else {
				candidate := strings.ToUpper(name[:1])
				good := obj(state["responses"])[candidate] == "good"
				if jev {
					p := .1
					if good {
						p = .9
					}
					answers[name] = M{"noul": p}
				} else {
					answers[name] = good
				}
			}
		}
		if jev {
			json.NewEncoder(w).Encode(M{"answers": answers, "usage": M{"input_tokens": 100, "output_tokens": 10}})
		} else {
			json.NewEncoder(w).Encode(M{"choices": []any{M{"finish_reason": "stop", "message": M{"content": string(Canon(answers))}}}, "usage": M{"prompt_tokens": 100, "completion_tokens": 10}})
		}
	}))
	defer server.Close()
	for _, method := range []string{"Compact", "Atomic"} {
		for _, model := range []string{"jev-latest", "m"} {
			c := NewClient(t.TempDir(), nativeConfig(), false)
			c.TypesafeURL, c.FireworksURL = server.URL, server.URL
			row := M{"input": "Follow the task", "output_1": "good", "output_2": "bad", "label": "1", "private_gold": "secret-gold-marker"}
			got := nativePair(row, method, model, c, "test")
			if !yes(got["valid"]) || num(got["published_score"]) != 1 || arr(got["decisions"])[0] != "1" || arr(got["decisions"])[1] != "1" {
				t.Fatal(method, model, got)
			}
		}
	}
}
func TestAtomicAdherencePriorityAndTie(t *testing.T) {
	cfg := nativeConfig()
	answers := M{"a_adherence": true, "a_correctness": false, "a_relevance": false, "b_adherence": false, "b_correctness": true, "b_relevance": true}
	d, _ := nativeDecision("Atomic", answers, cfg)
	if d != "1" {
		t.Fatal(d)
	}
	for k := range answers {
		answers[k] = true
	}
	d, _ = nativeDecision("Atomic", answers, cfg)
	if d != "TIE" {
		t.Fatal(d)
	}
	delete(answers, "a_adherence")
	d, _ = nativeDecision("Atomic", answers, cfg)
	if d != nil {
		t.Fatal("missing criterion was scored")
	}
}
