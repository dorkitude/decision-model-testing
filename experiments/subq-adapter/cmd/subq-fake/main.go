// Command subq-fake serves an offline stand-in for the SubQ System
// One decisions API (public preview) on localhost, default
// 127.0.0.1:8798. It exists only to exercise each experiment's
// subq wiring end to end. Its answers carry no signal; never
// report them. Point SUBQ_URL at
// http://127.0.0.1:8798/v1/systemone and set
// SUBQ_MODEL=fake-systemone and any SUBQ_API_KEY.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dorkitude/decision-model-testing/experiments/subq-adapter/subq/subqfake"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8798", "listen address")
	models := flag.String("models", subqfake.DefaultModel, "comma-separated model names to serve")
	failEvery := flag.Int("fail-every", 0, "fail every Nth valid request (odd: 503, even: 429, Retry-After: 0)")
	delay := flag.Duration("delay", 0, "sleep before each answer")
	flag.Parse()
	s := &subqfake.Server{Models: strings.Split(*models, ","), FailEvery: *failEvery, Delay: *delay}
	log.Printf("fake SubQ Snap on http://%s/v1/systemone, models %s (answers carry no signal)", *addr, *models)
	srv := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
