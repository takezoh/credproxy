package script

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/takezoh/credproxy/credproxy"
)

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
		Headers: resp.Headers,
		Query:   resp.Query,
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
