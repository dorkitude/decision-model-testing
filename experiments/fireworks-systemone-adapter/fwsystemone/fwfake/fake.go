// Package fwfake is an offline stand-in for the Fireworks System One
// decisions API (public). It reproduces the
// behavior observed on the live server and in the API schema: body
// validation before model resolution (422 for shape errors, 400 for limits,
// reasoning_effort and images), 404 NOT_FOUND for an unknown model, the
// standard Fireworks error body with request_id, the x-request-id and
// fireworks-server-processing-time headers, Retry-After on 429/503, and the
// documented answer and usage shapes. Its answers are deterministic
// pseudo-random numbers and carry no signal: never report them.
package fwfake

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/fireworks-systemone-adapter/fwsystemone"
)

// DefaultModel is the only model the fake serves unless Models is set.
const DefaultModel = "fake-systemone"

// Server configures the fake.
type Server struct {
	Models []string // served model names (default DefaultModel)
	// FailEvery makes every Nth POST that passes validation fail: odd N with
	// 503, even N with 429, both with Retry-After: 0.
	FailEvery int
	Delay     time.Duration // sleep before answering
	// OmitOptional drops the answer fields the experiments do not need
	// (confidence, legend) and usage.cached_input_tokens, to test that
	// parsers tolerate their absence.
	OmitOptional bool

	posts  atomic.Int64
	mu     sync.Mutex
	states map[string]bool // state hashes seen, for cached_input_tokens
}

// Posts returns the number of POST requests received.
func (s *Server) Posts() int { return int(s.posts.Load()) }

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/inference/v1/systemone", s.systemone)
	mux.HandleFunc("/v1/systemone", s.systemone)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, 404, "NOT_FOUND", "not found", nil) })
	return mux
}

func rid() string {
	h := sha256.Sum256([]byte(time.Now().String()))
	x := hex.EncodeToString(h[:16])
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

func (s *Server) send(w http.ResponseWriter, code int, v any, id string) {
	b, _ := json.Marshal(v)
	w.Header().Set("content-type", "application/json")
	w.Header().Set("x-request-id", id)
	w.Header().Set(fwsystemone.FakeHeader, "1")
	w.WriteHeader(code)
	w.Write(b)
}

func (s *Server) fail(w http.ResponseWriter, code int, kind, msg string, param any) {
	id := rid()
	s.send(w, code, map[string]any{"error": map[string]any{"message": msg, "param": param, "code": kind, "type": "error"}, "request_id": id}, id)
}

func (s *Server) systemone(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.fail(w, 404, "NOT_FOUND", "not found", nil)
		return
	}
	n := s.posts.Add(1)
	body, _ := io.ReadAll(io.LimitReader(r.Body, fwsystemone.MaxBodyBytes+1))
	if len(body) > fwsystemone.MaxBodyBytes {
		s.fail(w, 413, "PAYLOAD_TOO_LARGE", "request body over 8 MiB", nil)
		return
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		s.fail(w, 422, "UNPROCESSABLE_ENTITY", "request body must be a JSON object", nil)
		return
	}
	var model string
	if v, ok := top["model"]; !ok || json.Unmarshal(v, &model) != nil || model == "" {
		s.fail(w, 422, "UNPROCESSABLE_ENTITY", "model is required", nil)
		return
	}
	if v, ok := top["reasoning_effort"]; ok && string(v) != `"none"` {
		s.fail(w, 400, "BAD_REQUEST", "reasoning_effort is not supported", nil)
		return
	}
	if _, ok := top["images"]; ok {
		s.fail(w, 400, "BAD_REQUEST", "images are not supported", nil)
		return
	}
	qs, e := fwsystemone.Questions(body)
	if e != nil {
		code, kind := 422, "UNPROCESSABLE_ENTITY"
		if strings.Contains(e.Error(), "at most") || strings.Contains(e.Error(), "-byte limit") {
			code, kind = 400, "BAD_REQUEST"
		}
		s.fail(w, code, kind, e.Error(), nil)
		return
	}
	models := s.Models
	if len(models) == 0 {
		models = []string{DefaultModel}
	}
	known := false
	for _, m := range models {
		if m == model {
			known = true
		}
	}
	if !known {
		s.fail(w, 404, "NOT_FOUND", "Model not found, inaccessible, and/or not deployed", "model")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		s.fail(w, 401, "UNAUTHORIZED", "missing or invalid API key", nil)
		return
	}
	if s.FailEvery > 0 && n%int64(s.FailEvery) == 0 {
		w.Header().Set("Retry-After", "0")
		if s.FailEvery%2 == 1 {
			s.fail(w, 503, "SERVICE_UNAVAILABLE", "fake: temporarily at capacity", nil)
		} else {
			s.fail(w, 429, "TOO_MANY_REQUESTS", "fake: rate limited", nil)
		}
		return
	}
	start := time.Now()
	if s.Delay > 0 {
		time.Sleep(s.Delay)
	}
	var qraw map[string]json.RawMessage
	json.Unmarshal(top["questions"], &qraw)
	state := top["state"]
	answers := map[string]any{}
	for _, q := range qs {
		seed := string(state) + "\x00" + q.Key + "\x00" + string(qraw[q.Key])
		switch q.Type {
		case "noul":
			answers[q.Key] = map[string]any{"type": "noul", "noul": r4(probs(seed, 2)[1])}
		case "choice":
			p := probs(seed, len(q.Options))
			pm := map[string]float64{}
			for j, k := range q.Options {
				pm[k] = r4(p[j])
			}
			answers[q.Key] = map[string]any{"type": "choice", "choice": q.Options[argmax(p)], "probabilities": pm, "confidence": r4(conf(p))}
		case "score":
			p := probs(seed, len(q.Options))
			pm, legend := map[string]float64{}, map[string]string{}
			var crit struct {
				Criteria []string `json:"criteria"`
			}
			json.Unmarshal(qraw[q.Key], &crit)
			levels := crit.Criteria
			ev := 0.0
			for j := range p {
				ev += float64(j) * p[j]
				pm[strconv.Itoa(j)] = r4(p[j])
				if j < len(levels) {
					legend[strconv.Itoa(j)] = levels[j]
				}
			}
			answers[q.Key] = map[string]any{"type": "score", "score": r4(ev), "legend": legend, "probabilities": pm, "confidence": r4(conf(p))}
		}
	}
	sh := sha256.Sum256(state)
	key := hex.EncodeToString(sh[:])
	s.mu.Lock()
	if s.states == nil {
		s.states = map[string]bool{}
	}
	cached := 0
	if s.states[key] {
		cached = len(state) / 4
	}
	s.states[key] = true
	s.mu.Unlock()
	if s.OmitOptional {
		for _, a := range answers {
			m := a.(map[string]any)
			delete(m, "confidence")
			delete(m, "legend")
		}
	}
	usage := map[string]int{"input_tokens": len(body) / 4, "cached_input_tokens": cached, "output_tokens": 0}
	if s.OmitOptional {
		delete(usage, "cached_input_tokens")
	}
	w.Header().Set("fireworks-server-processing-time", fmt.Sprintf("%.3f", time.Since(start).Seconds()+0.05))
	id := rid()
	s.send(w, 200, map[string]any{"model": model, "answers": answers,
		"usage": usage}, id)
}

func r4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

func probs(seed string, n int) []float64 {
	p := make([]float64, n)
	sum := 0.0
	for i := range p {
		h := sha256.Sum256([]byte(seed + "\x00" + strconv.Itoa(i)))
		v := math.Exp(3 * float64(binary.BigEndian.Uint16(h[:])) / 65535)
		p[i] = v
		sum += v
	}
	for i := range p {
		p[i] /= sum
	}
	return p
}

func argmax(p []float64) int {
	b := 0
	for i := range p {
		if p[i] > p[b] {
			b = i
		}
	}
	return b
}

// conf is 0 for a uniform distribution and 1 for a point mass (normalized
// entropy complement), matching the documented meaning of confidence.
func conf(p []float64) float64 {
	if len(p) < 2 {
		return 1
	}
	h := 0.0
	for _, x := range p {
		if x > 0 {
			h -= x * math.Log(x)
		}
	}
	return math.Max(0, 1-h/math.Log(float64(len(p))))
}
