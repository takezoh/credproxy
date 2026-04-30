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

func TestMatchTokenEntries(t *testing.T) {
	entries := []tokenEntry{
		{token: []byte("alpha"), id: "id-alpha"},
		{token: []byte("beta"), id: "id-beta"},
	}
	tests := []struct {
		presented []byte
		wantID    string
		wantOK    bool
	}{
		{[]byte("alpha"), "id-alpha", true},
		{[]byte("beta"), "id-beta", true},
		{[]byte("gamma"), "", false},
		{[]byte(""), "", false},
		{[]byte("alph"), "", false},
		{[]byte("alphax"), "", false},
	}
	for _, tt := range tests {
		gotID, gotOK := matchTokenEntries(entries, tt.presented)
		if gotOK != tt.wantOK || gotID != tt.wantID {
			t.Errorf("matchTokenEntries(%q) = (%q, %v), want (%q, %v)", tt.presented, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}
