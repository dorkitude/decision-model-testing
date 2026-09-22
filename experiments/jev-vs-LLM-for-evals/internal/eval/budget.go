package eval

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// Meter reserves estimated cost and an attempt BEFORE network dispatch. An
// unanswered reservation survives process death and remains charged to the cap.
// This is a conservative operational estimate, not a provider billing guarantee.
type Meter struct {
	mu              sync.Mutex
	Config, Pricing M
	Attempts        int
	CommittedUSD    float64
	Fatal           string
}

func NewMeter(config, pricing M) *Meter { return &Meter{Config: config, Pricing: pricing} }
func reserveEstimate(model string, payload, pricing M) (float64, error) {
	rate := obj(obj(pricing["models"])[model])
	if rate["input_per_million"] == nil || rate["output_per_million"] == nil {
		return 0, fmt.Errorf("no price configured for %s", model)
	}
	// Byte count exceeds ordinary text token counts; allowance covers wrappers.
	// Actual reported cost replaces this estimate when available. No cache discount.
	input := float64(len(Canon(payload)) + 2048)
	output := num(payload["max_tokens"])
	return (input*num(rate["input_per_million"]) + output*num(rate["output_per_million"])) / 1e6, nil
}
func (m *Meter) Restore(out string) {
	reservations := map[string]float64{}
	count := 0
	spent := 0.0
	for _, r := range Lines(filepath.Join(out, "reservations.jsonl")) {
		id := str(r["attempt_id"])
		if _, ok := reservations[id]; ok {
			panic("duplicate reservation ID")
		}
		v := num(r["reserved_usd"])
		reservations[id] = v
		spent += v
		count++
	}
	settled := map[string]bool{}
	for _, r := range Lines(filepath.Join(out, "requests.jsonl")) {
		id := str(r["attempt_id"])
		if id != "" && settled[id] {
			panic("duplicate attempt receipt")
		}
		settled[id] = true
		cost := PriceReceipt(r, m.Pricing)["estimated_total_usd"]
		if reserve, ok := reservations[id]; ok {
			if cost != nil {
				spent += num(cost) - reserve
			}
		} else {
			count++
			if cost != nil {
				spent += num(cost)
			} else {
				estimate, e := reserveEstimate(str(r["model"]), obj(r["payload"]), m.Pricing)
				if e != nil && num(m.Config["max_estimated_usd"]) > 0 {
					panic(e)
				}
				spent += estimate
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Attempts += count
	m.CommittedUSD += spent
}
func (m *Meter) Reserve(out, key, model string, payload M) M {
	amount, e := reserveEstimate(model, payload, m.Pricing)
	if e != nil && num(m.Config["max_estimated_usd"]) > 0 {
		panic(e)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fatal != "" {
		panic(fmt.Errorf("campaign stopped: %s", m.Fatal))
	}
	if m.Attempts >= int(num(m.Config["max_requests"])) {
		panic("request cap reached")
	}
	if cap := num(m.Config["max_estimated_usd"]); cap > 0 && m.CommittedUSD+amount > cap {
		panic("estimated cost reservation cap reached")
	}
	id := make([]byte, 16)
	_, e = rand.Read(id)
	check(e)
	r := M{"attempt_id": hex.EncodeToString(id), "key": key, "model": model, "payload_sha256": Hash(Canon(payload)), "reserved_usd": amount, "reserved_utc": time.Now().UTC().Format(time.RFC3339Nano)}
	Append(filepath.Join(out, "reservations.jsonl"), r)
	m.Attempts++
	m.CommittedUSD += amount
	return r
}
func (m *Meter) Settle(row M) {
	cost := PriceReceipt(row, m.Pricing)["estimated_total_usd"]
	m.mu.Lock()
	defer m.mu.Unlock()
	if cost != nil {
		m.CommittedUSD += num(cost) - num(row["reserved_usd"])
	}
	if num(row["http_status"]) == 401 || num(row["http_status"]) == 403 {
		m.Fatal = fmt.Sprintf("provider authentication/authorization HTTP %.0f", num(row["http_status"]))
	}
}
func (m *Meter) Snapshot() M {
	m.mu.Lock()
	defer m.mu.Unlock()
	return M{"reserved_attempts": m.Attempts, "committed_estimated_usd": m.CommittedUSD, "max_requests": m.Config["max_requests"], "max_estimated_usd": m.Config["max_estimated_usd"], "fatal": m.Fatal, "basis": "actual list-price cost where usage is known, otherwise retained preflight reservation; not an invoice guarantee"}
}
