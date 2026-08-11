package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/takezoh/credproxy/credproxy"
)

// reasonPrefix marks a machine-readable failure classification on the first
// stderr line of a failing hook: "reason:<token>", token in [a-z0-9_]{1,64}.
const reasonPrefix = "reason:"

// maxReasonLen bounds the reason token so an arbitrary stderr line can never
// ride into the client-facing response through this channel.
const maxReasonLen = 64

// parseReason extracts the failure classification from hook stderr.
// Anything that does not match the strict token grammar is treated as
// "no reason" — free-form stderr must stay server-side.
func parseReason(stderr []byte) (string, bool) {
	line := stderr
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte(reasonPrefix)) {
		return "", false
	}
	token := string(line[len(reasonPrefix):])
	if len(token) == 0 || len(token) > maxReasonLen {
		return "", false
	}
	for _, c := range token {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return "", false
		}
	}
	return token, true
}

// parseHookResponse decodes hook stdout into an Injection and an optional cache expiry.
// safety is subtracted from expires_in_sec to compute the cache deadline; typical value is 30s.
// Returns a zero expiry when the response should not be cached.
func parseHookResponse(stdout []byte, now time.Time, safety time.Duration) (*credproxy.Injection, time.Time, error) {
	var resp hookResponse
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&resp); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode hook response: %w", err)
	}

	inj := &credproxy.Injection{
		Headers:       resp.Headers,
		AppendHeaders: resp.AppendHeaders,
		Query:         resp.Query,
	}

	// Normalize body_replace: null JSON token and empty object are treated as absent.
	raw := []byte(resp.BodyReplace)
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		inj.BodyReplace = raw
	}

	var cacheUntil time.Time
	if ttl := time.Duration(resp.ExpiresInSec)*time.Second - safety; ttl > 0 {
		cacheUntil = now.Add(ttl)
		inj.ExpiresAt = cacheUntil
	}

	return inj, cacheUntil, nil
}
