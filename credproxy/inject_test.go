package credproxy

import (
	"testing"
)

func TestDecideAction(t *testing.T) {
	tests := []struct {
		name string
		cfg  Route
		inj  *Injection
		want action
	}{
		{
			name: "BodyReplace present",
			cfg:  Route{Upstream: "https://example.com"},
			inj:  &Injection{BodyReplace: []byte(`{"ok":true}`)},
			want: actReturnBody,
		},
		{
			name: "forward to upstream",
			cfg:  Route{Upstream: "https://example.com"},
			inj:  &Injection{Headers: map[string]string{"Authorization": "Bearer x"}},
			want: actForward,
		},
		{
			name: "no upstream no body replace",
			cfg:  Route{Upstream: ""},
			inj:  &Injection{},
			want: actNoUpstream,
		},
		{
			name: "BodyReplace takes priority over upstream",
			cfg:  Route{Upstream: "https://example.com"},
			inj:  &Injection{BodyReplace: []byte("x"), Headers: map[string]string{"X-A": "b"}},
			want: actReturnBody,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideAction(tt.cfg, tt.inj); got != tt.want {
				t.Errorf("decideAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanRequest(t *testing.T) {
	t.Run("injects headers and query", func(t *testing.T) {
		cfg := Route{StripInboundAuth: false}
		inj := &Injection{
			Headers: map[string]string{"Authorization": "Bearer tok", "X-Extra": "v"},
			Query:   map[string]string{"api_key": "abc"},
		}
		p := planRequest(cfg, inj)
		if p.setHeaders["Authorization"] != "Bearer tok" {
			t.Errorf("Authorization header not set: %v", p.setHeaders)
		}
		if p.setQuery["api_key"] != "abc" {
			t.Errorf("query not set: %v", p.setQuery)
		}
		if len(p.deleteHeaders) != 0 {
			t.Errorf("unexpected deleteHeaders: %v", p.deleteHeaders)
		}
	})

	t.Run("StripInboundAuth adds Authorization to deleteHeaders", func(t *testing.T) {
		cfg := Route{StripInboundAuth: true}
		inj := &Injection{Headers: map[string]string{"Authorization": "Bearer new"}}
		p := planRequest(cfg, inj)
		if len(p.deleteHeaders) != 1 || p.deleteHeaders[0] != "Authorization" {
			t.Errorf("deleteHeaders = %v, want [Authorization]", p.deleteHeaders)
		}
		if p.setHeaders["Authorization"] != "Bearer new" {
			t.Errorf("inject header not set after strip: %v", p.setHeaders)
		}
	})

	t.Run("AppendHeaders populate mergeHeaders", func(t *testing.T) {
		inj := &Injection{
			Headers:       map[string]string{"Authorization": "Bearer tok"},
			AppendHeaders: map[string]string{"anthropic-beta": "oauth-2025-04-20"},
		}
		p := planRequest(Route{}, inj)
		if p.setHeaders["Authorization"] != "Bearer tok" {
			t.Errorf("Authorization not set: %v", p.setHeaders)
		}
		if p.mergeHeaders["anthropic-beta"] != "oauth-2025-04-20" {
			t.Errorf("mergeHeaders = %v, want anthropic-beta", p.mergeHeaders)
		}
	})

	t.Run("empty injection", func(t *testing.T) {
		p := planRequest(Route{}, &Injection{})
		if len(p.setHeaders) != 0 || len(p.setQuery) != 0 || len(p.mergeHeaders) != 0 {
			t.Errorf("unexpected non-empty plan: %+v", p)
		}
	})
}

func TestMergeCSV(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		add      string
		want     string
	}{
		{"empty existing", "", "oauth-2025-04-20", "oauth-2025-04-20"},
		{"append to existing", "fine-grained-tool-streaming-2025-05-14", "oauth-2025-04-20",
			"fine-grained-tool-streaming-2025-05-14, oauth-2025-04-20"},
		{"dedup already present", "oauth-2025-04-20, context-1m-2025-08-07", "oauth-2025-04-20",
			"oauth-2025-04-20, context-1m-2025-08-07"},
		{"trims spaces", "a ,  b", "c", "a, b, c"},
		{"empty add keeps existing", "a, b", "", "a, b"},
		{"both empty", "", "", ""},
		{"multi-token add", "a", "b, c", "a, b, c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeCSV(tt.existing, tt.add); got != tt.want {
				t.Errorf("mergeCSV(%q, %q) = %q, want %q", tt.existing, tt.add, got, tt.want)
			}
		})
	}
}

func TestNeedsRefresh(t *testing.T) {
	tests := []struct {
		refreshOn []int
		status    int
		want      bool
	}{
		{[]int{401, 403}, 401, true},
		{[]int{401, 403}, 403, true},
		{[]int{401}, 200, false},
		{nil, 401, false},
		{[]int{}, 401, false},
	}
	for _, tt := range tests {
		if got := needsRefresh(tt.refreshOn, tt.status); got != tt.want {
			t.Errorf("needsRefresh(%v, %d) = %v, want %v", tt.refreshOn, tt.status, got, tt.want)
		}
	}
}
