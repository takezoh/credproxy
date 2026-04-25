package script

import (
	"encoding/json"
	"testing"

	"github.com/takezoh/credproxy/pkg/credproxy"
)

func TestBuildHookRequest(t *testing.T) {
	req := credproxy.Request{
		Method: "POST",
		Path:   "/v1/messages",
		Host:   "api.anthropic.com",
		Metadata: map[string]string{
			"client":       "my-app",
			"project_path": "/workspace/foo",
		},
	}
	b, err := buildHookRequest("get", "anthropic", req)
	if err != nil {
		t.Fatalf("buildHookRequest: %v", err)
	}

	var got hookRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != "get" {
		t.Errorf("Action = %q, want get", got.Action)
	}
	if got.Route != "anthropic" {
		t.Errorf("Route = %q, want anthropic", got.Route)
	}
	if got.Request.Method != "POST" || got.Request.Path != "/v1/messages" {
		t.Errorf("Request = %+v", got.Request)
	}
	if got.Context.Client != "my-app" {
		t.Errorf("Context.Client = %q, want my-app", got.Context.Client)
	}
	if got.Context.ProjectPath != "/workspace/foo" {
		t.Errorf("Context.ProjectPath = %q, want /workspace/foo", got.Context.ProjectPath)
	}
}

func TestBuildHookRequest_emptyMetadata(t *testing.T) {
	req := credproxy.Request{Method: "GET"}
	b, err := buildHookRequest("refresh", "route", req)
	if err != nil {
		t.Fatalf("buildHookRequest: %v", err)
	}
	var got hookRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Context.Client != "" || got.Context.ProjectPath != "" {
		t.Errorf("expected empty context, got %+v", got.Context)
	}
}
