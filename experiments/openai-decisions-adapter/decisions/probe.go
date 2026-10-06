package decisions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Access states reported by Probe.
const (
	AccessNotEnabled = "not_enabled" // endpoint exists, account not in the preview
	AccessOpen       = "open"        // the request reached body validation or succeeded
	AccessAuthError  = "auth_error"
	AccessMissing    = "endpoint_missing"
	AccessUnknown    = "unknown"
)

// ProbeResult is the outcome of a free access check.
type ProbeResult struct {
	Checked string `json:"checked_utc"`
	URL     string `json:"url"`
	Status  int    `json:"status"`
	Access  string `json:"access"`
	Message string `json:"message"`
}

// Probe posts an empty body. It never runs inference: an enabled account gets
// a validation error, which is itself the signal that access is open.
func Probe(ctx context.Context, h *http.Client, url, token string) (ProbeResult, error) {
	if url == "" {
		url = Endpoint
	}
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	r := ProbeResult{Checked: time.Now().UTC().Format(time.RFC3339), URL: url}
	req, e := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
	if e != nil {
		return r, e
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := h.Do(req)
	if e != nil {
		return r, e
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	r.Status = resp.StatusCode
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(b, &body)
	r.Message = body.Error.Message
	r.Access = Classify(r.Status, r.Message)
	return r, nil
}

// Classify maps a probe response to an access state.
func Classify(status int, msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "not enabled"):
		return AccessNotEnabled
	case status == 401 || status == 403:
		return AccessAuthError
	case status == 404:
		return AccessMissing
	case status >= 200 && status < 300, status == 400, status == 422:
		return AccessOpen
	}
	return AccessUnknown
}
