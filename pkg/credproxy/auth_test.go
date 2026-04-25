package credproxy

import (
	"testing"
)

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		name      string
		values    []string
		wantToken string
		wantOK    bool
	}{
		{"valid", []string{"Bearer mytoken"}, "mytoken", true},
		{"valid lowercase scheme", []string{"bearer mytoken"}, "mytoken", true},
		{"valid mixed case", []string{"BEARER mytoken"}, "mytoken", true},
		{"token with spaces trimmed", []string{"Bearer  tok "}, "tok", true},
		{"empty header list", []string{}, "", false},
		{"multiple values", []string{"Bearer a", "Bearer b"}, "", false},
		{"no bearer prefix", []string{"Basic dXNlcjpwYXNz"}, "", false},
		{"bare token no scheme", []string{"mytoken"}, "", false},
		{"bearer only no token", []string{"Bearer "}, "", false},
		{"bearer with only spaces", []string{"Bearer   "}, "", false},
		{"empty string", []string{""}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractBearer(tt.values)
			if ok != tt.wantOK || got != tt.wantToken {
				t.Errorf("extractBearer(%v) = (%q, %v), want (%q, %v)", tt.values, got, ok, tt.wantToken, tt.wantOK)
			}
		})
	}
}

func TestMatchToken(t *testing.T) {
	tokens := [][]byte{[]byte("alpha"), []byte("beta")}
	tests := []struct {
		presented []byte
		want      bool
	}{
		{[]byte("alpha"), true},
		{[]byte("beta"), true},
		{[]byte("gamma"), false},
		{[]byte(""), false},
		{[]byte("alph"), false},
		{[]byte("alphax"), false},
	}
	for _, tt := range tests {
		if got := matchToken(tokens, tt.presented); got != tt.want {
			t.Errorf("matchToken(%q) = %v, want %v", tt.presented, got, tt.want)
		}
	}
}
