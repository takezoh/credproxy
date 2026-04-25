package script

import (
	"testing"
	"time"
)

var testNow = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
var testSafety = 30 * time.Second

func TestParseHookResponse_headers(t *testing.T) {
	stdout := []byte(`{"headers":{"Authorization":"Bearer tok"},"query":{"k":"v"},"expires_in_sec":3600}`)
	inj, until, err := parseHookResponse(stdout, testNow, testSafety)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inj.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Authorization = %q", inj.Headers["Authorization"])
	}
	if inj.Query["k"] != "v" {
		t.Errorf("Query[k] = %q", inj.Query["k"])
	}
	expected := testNow.Add(3600*time.Second - testSafety)
	if !until.Equal(expected) {
		t.Errorf("cacheUntil = %v, want %v", until, expected)
	}
}

func TestParseHookResponse_bodyReplace_valid(t *testing.T) {
	stdout := []byte(`{"body_replace":{"AccessKeyId":"AK"}}`)
	inj, _, err := parseHookResponse(stdout, testNow, testSafety)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inj.BodyReplace) == 0 {
		t.Error("expected BodyReplace to be set")
	}
}

func TestParseHookResponse_bodyReplace_null(t *testing.T) {
	stdout := []byte(`{"body_replace":null}`)
	inj, _, err := parseHookResponse(stdout, testNow, testSafety)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inj.BodyReplace != nil {
		t.Errorf("expected nil BodyReplace for null, got %q", inj.BodyReplace)
	}
}

func TestParseHookResponse_bodyReplace_emptyObject(t *testing.T) {
	stdout := []byte(`{"body_replace":{}}`)
	inj, _, err := parseHookResponse(stdout, testNow, testSafety)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inj.BodyReplace != nil {
		t.Errorf("expected nil BodyReplace for empty object, got %q", inj.BodyReplace)
	}
}

func TestParseHookResponse_expiresInSec_boundary(t *testing.T) {
	tests := []struct {
		name       string
		expiresIn  int
		wantCached bool
	}{
		{"zero", 0, false},
		{"negative", -1, false},
		{"exactly safety (30s)", 30, false},
		{"safety+1 (31s)", 31, true},
		{"large", 3600, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := []byte(`{"expires_in_sec":` + itoa(tt.expiresIn) + `}`)
			_, until, err := parseHookResponse(stdout, testNow, testSafety)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cached := !until.IsZero(); cached != tt.wantCached {
				t.Errorf("cacheUntil.IsZero=%v, wantCached=%v", until.IsZero(), tt.wantCached)
			}
		})
	}
}

func TestParseHookResponse_unknownField(t *testing.T) {
	stdout := []byte(`{"unknown_field":"surprise"}`)
	_, _, err := parseHookResponse(stdout, testNow, testSafety)
	if err == nil {
		t.Error("expected error for unknown field with DisallowUnknownFields")
	}
}

func TestParseHookResponse_invalidJSON(t *testing.T) {
	_, _, err := parseHookResponse([]byte(`not json`), testNow, testSafety)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
