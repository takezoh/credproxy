package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		c       Config
		wantErr bool
	}{
		{
			name: "valid minimal",
			c: Config{
				ListenTCP: "127.0.0.1:9787",
				Routes:    []Route{{Path: "/x", Upstream: "https://example.com"}},
			},
		},
		{
			name: "valid unix only",
			c: Config{
				ListenUnix: "/tmp/test.sock",
				Routes:     []Route{{Path: "/x", Upstream: "https://example.com"}},
			},
		},
		{
			name: "valid credential_command only (no upstream)",
			c: Config{
				ListenTCP: "127.0.0.1:9787",
				Routes:    []Route{{Path: "/x", CredentialCommand: []string{"hook.sh"}}},
			},
		},
		{
			name:    "no listeners",
			c:       Config{Routes: []Route{{Path: "/x", Upstream: "https://example.com"}}},
			wantErr: true,
		},
		{
			name: "missing route path",
			c: Config{
				ListenTCP: "127.0.0.1:9787",
				Routes:    []Route{{Upstream: "https://example.com"}},
			},
			wantErr: true,
		},
		{
			name: "route missing upstream and credential_command",
			c: Config{
				ListenTCP: "127.0.0.1:9787",
				Routes:    []Route{{Path: "/x"}},
			},
			wantErr: true,
		},
		{
			name: "no routes is valid",
			c:    Config{ListenTCP: "127.0.0.1:9787"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
