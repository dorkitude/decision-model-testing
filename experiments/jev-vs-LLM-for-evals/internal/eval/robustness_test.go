package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBudgetReservationSurvivesUnansweredAttempt(t *testing.T) {
	out := t.TempDir()
	cfg := M{"max_requests": 2, "max_estimated_usd": 1.0}
	m := NewMeter(cfg, prices())
	r := m.Reserve(out, "job/a", "m", M{"max_tokens": 10})
	m2 := NewMeter(cfg, prices())
	m2.Restore(out)
	if m2.Attempts != 1 || m2.CommittedUSD != num(r["reserved_usd"]) {
		t.Fatal(m2.Snapshot())
	}
	reply := receipt("m", M{"input_tokens": 100.0, "output_tokens": 10.0})
	reply["attempt_id"], reply["reserved_usd"] = r["attempt_id"], r["reserved_usd"]
	Append(filepath.Join(out, "requests.jsonl"), reply)
	m3 := NewMeter(cfg, prices())
	m3.Restore(out)
	if m3.Attempts != 1 || m3.CommittedUSD >= m2.CommittedUSD {
		t.Fatal(m3.Snapshot())
	}
}
func TestBudgetCapIncludesInflightReservations(t *testing.T) {
	out := t.TempDir()
	payload := M{"max_tokens": 10}
	estimate, e := reserveEstimate("m", payload, prices())
	if e != nil {
		t.Fatal(e)
	}
	m := NewMeter(M{"max_requests": 5, "max_estimated_usd": estimate * 1.5}, prices())
	m.Reserve(out, "a", "m", payload)
	defer func() {
		if recover() == nil {
			t.Error("inflight cost cap bypassed")
		}
	}()
	m.Reserve(out, "b", "m", payload)
}
func TestRepairTailPreservesCorruptionEvidence(t *testing.T) {
	out := t.TempDir()
	p := filepath.Join(out, "r.jsonl")
	os.WriteFile(p, []byte("{\"ok\":true}\n{\"torn\":"), 0644)
	RepairTail(p)
	if len(Lines(p)) != 1 {
		t.Fatal("lost complete record")
	}
	matches, _ := filepath.Glob(p + ".torn-*")
	if len(matches) != 1 || string(Read(matches[0])) != "{\"torn\":" {
		t.Fatal("missing evidence")
	}
	os.WriteFile(p, []byte("{\"ok\":true}"), 0644)
	RepairTail(p)
	if !strings.HasSuffix(string(Read(p)), "\n") {
		t.Fatal("valid tail not completed")
	}
}
func TestArchiveRoundTripAndTransparentRead(t *testing.T) {
	out := t.TempDir()
	p := filepath.Join(out, "large.jsonl")
	r := M{"text": strings.Repeat("long repeated input ", 60000)}
	Append(p, r)
	before := Hash(Read(p))
	idx := SealShard(out)
	if len(idx) != 1 {
		t.Fatal(idx)
	}
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("raw duplicate retained")
	}
	if Hash(Read(p)) != before || Hash(Read(p+".gz")) != before || len(Lines(p)) != 1 {
		t.Fatal("archive changed")
	}
}
func TestCancellationDoesNotCacheTerminalFailure(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-only")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	out := t.TempDir()
	c := NewClient(out, M{"timeout_s": 2, "max_requests": 3}, false)
	c.FireworksURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c.Context = ctx
	func() {
		defer func() {
			if recover() == nil {
				t.Error("cancelled request returned a final judgment")
			}
		}()
		c.Request("j/x", "m", M{})
	}()
	rows := Lines(filepath.Join(out, "requests.jsonl"))
	if len(rows) != 1 || yes(rows[0]["terminal"]) {
		t.Fatal(rows)
	}
	resumed := NewClient(out, c.Config, false)
	if len(resumed.cache) != 0 {
		t.Fatal("cancelled failure poisoned resume")
	}
}
func TestHTTPAuthenticationStopsFurtherCalls(t *testing.T) {
	m := NewMeter(M{"max_requests": 10}, M{})
	m.Settle(M{"model": "m", "http_status": 401})
	defer func() {
		if recover() == nil {
			t.Error("auth error failed to stop")
		}
	}()
	m.Reserve(t.TempDir(), "j/x", "m", M{})
}
func TestExplicitNullAndMalformedUsageAreNotFree(t *testing.T) {
	for _, u := range []M{{"input_tokens": nil, "output_tokens": nil}, {"input_tokens": "100", "output_tokens": 10.0}, {"input_tokens": 100.0, "output_tokens": -1.0}} {
		r := PriceReceipt(receipt("m", u), prices())
		if r["estimated_total_usd"] != nil {
			t.Fatal(r)
		}
	}
}
