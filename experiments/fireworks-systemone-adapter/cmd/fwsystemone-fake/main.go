// Command fwsystemone-fake serves an offline stand-in for the Fireworks System
// One decisions API (public) on localhost, default
// 127.0.0.1:8798. It exists only to exercise each experiment's
// fireworks-systemone wiring end to end. Its answers carry no signal; never
// report them. Point FIREWORKS_SYSTEMONE_URL at
// http://127.0.0.1:8798/inference/v1/systemone and set
// FIREWORKS_SYSTEMONE_MODEL=fake-systemone and any FIREWORKS_API_KEY.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/fireworks-systemone-adapter/fwsystemone/fwfake"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8798", "listen address")
	models := flag.String("models", fwfake.DefaultModel, "comma-separated model names to serve")
	failEvery := flag.Int("fail-every", 0, "fail every Nth valid request (odd: 503, even: 429, Retry-After: 0)")
	delay := flag.Duration("delay", 0, "sleep before each answer")
	flag.Parse()
	s := &fwfake.Server{Models: strings.Split(*models, ","), FailEvery: *failEvery, Delay: *delay}
	log.Printf("fake Fireworks System One on http://%s/inference/v1/systemone, models %s (answers carry no signal)", *addr, *models)
	srv := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
