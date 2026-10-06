// Command decisions-fake serves the documented Decisions API wire format
// (guide, 2026-10-06) on localhost with deterministic pseudo-random answers.
// It exists only to exercise each experiment's openai-decisions wiring end to
// end without credentials or cost. Its answers carry no signal; never report
// them.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"log"
	"net/http"
)

type question struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	Choices      []struct {
		Value string `json:"value"`
	} `json:"choices"`
	Levels []struct {
		Label string `json:"label"`
	} `json:"levels"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "listen address")
	flag.Parse()
	http.HandleFunc("/v1/decisions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model     string     `json:"model"`
			Input     string     `json:"input"`
			Questions []question `json:"questions"`
		}
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil || len(req.Questions) == 0 {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"Missing required parameter: 'questions'.","type":"invalid_request_error","param":"questions","code":"missing_required_parameter"}}`))
			return
		}
		// u returns a deterministic value in (0, 1) per question, option and input.
		u := func(q, opt string) float64 {
			h := sha256.Sum256([]byte(q + "\x00" + opt + "\x00" + req.Input))
			return (float64(binary.BigEndian.Uint16(h[:])) + 1) / 65537
		}
		var out []map[string]any
		for _, q := range req.Questions {
			switch q.Type {
			case "predicate":
				out = append(out, map[string]any{"type": "predicate", "name": q.Name, "probability": u(q.Name, "")})
			case "choice", "score":
				labels := []string{}
				for _, c := range q.Choices {
					labels = append(labels, c.Value)
				}
				for _, l := range q.Levels {
					labels = append(labels, l.Label)
				}
				ws, total := make([]float64, len(labels)), 0.0
				for i, l := range labels {
					ws[i] = u(q.Name, l)
					total += ws[i]
				}
				probs, best, expected := []map[string]any{}, 0, 0.0
				for i, l := range labels {
					ws[i] /= total
					if ws[i] > ws[best] {
						best = i
					}
					expected += float64(i) * ws[i]
					if q.Type == "choice" {
						probs = append(probs, map[string]any{"value": l, "probability": ws[i]})
					} else {
						probs = append(probs, map[string]any{"value": i, "label": l, "probability": ws[i]})
					}
				}
				a := map[string]any{"type": q.Type, "name": q.Name, "probabilities": probs, "confidence": ws[best]}
				if q.Type == "choice" {
					a["choice"] = labels[best]
				} else {
					a["score"] = expected
				}
				out = append(out, a)
			default:
				w.WriteHeader(400)
				w.Write([]byte(`{"error":{"message":"Invalid value: '` + q.Type + `'. Supported values are: 'predicate', 'choice', and 'score'.","type":"invalid_request_error"}}`))
				return
			}
		}
		n := len(req.Input) / 4
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "answers": out, "usage": map[string]int{"input_tokens": n, "output_tokens": 0, "total_tokens": n}})
	})
	log.Printf("fake decisions API on http://%s/v1/decisions", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
