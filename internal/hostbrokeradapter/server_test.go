package hostbrokeradapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteInventoryUsesMappedCallerAndRejectsOpenBackend(t *testing.T) {
	requests := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requests++
		json.NewEncoder(w).Encode(map[string]any{"routes": []string{"ctx-sync"}})
	}))
	defer backend.Close()
	dir := t.TempDir()
	secret := func(name, value string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cfg := Config{BackendURL: backend.URL, BearerFile: secret("adapter.token", "adapter"), StateFile: filepath.Join(dir, "journal.json"), PrincipalTokenFiles: map[string]string{"client-a": secret("backend.token", "backend")}}
	app, err := NewApplication(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	invoke := request{Key: wireKey{Principal: "client-a", Service: "credproxy", Operation: "list_routes", RequestID: "r1"}, Caller: caller{Principal: "client-a", InstanceGeneration: 1}, ContractMajor: 1, Input: base64.StdEncoding.EncodeToString([]byte(`{}`)), ResponseWindowBytes: 1024}
	raw, _ := json.Marshal(invoke)
	req := httptest.NewRequest(http.MethodPost, protocolPrefix+"Invoke", nil)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Host-Broker-Private-Protocol", "1")
	req.Header.Set("Authorization", "Bearer adapter")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || requests != 1 || !strings.Contains(recorder.Body.String(), `"kind":"completed"`) {
		t.Fatalf("response=%d %s requests=%d", recorder.Code, recorder.Body.String(), requests)
	}
	invoke.Key.Principal, invoke.Caller.Principal, invoke.Key.RequestID = "missing", "missing", "r2"
	raw, _ = json.Marshal(invoke)
	req = httptest.NewRequest(http.MethodPost, protocolPrefix+"Invoke", nil)
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Host-Broker-Private-Protocol", "1")
	req.Header.Set("Authorization", "Bearer adapter")
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	if requests != 1 || !strings.Contains(recorder.Body.String(), `"class":"permission_denied"`) {
		t.Fatalf("unmapped caller reached backend: %s", recorder.Body.String())
	}
}

func TestOpenBackendBlocksAdapterStartup(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer backend.Close()
	dir := t.TempDir()
	bearer := filepath.Join(dir, "bearer")
	os.WriteFile(bearer, []byte("x"), 0o600)
	_, err := NewApplication(context.Background(), Config{BackendURL: backend.URL, BearerFile: bearer, StateFile: filepath.Join(dir, "journal"), PrincipalTokenFiles: map[string]string{"a": bearer}})
	if err == nil {
		t.Fatal("open credproxy backend was accepted")
	}
}

func TestJournalReservesBeforeInvocationAndReplaysOnlyAfterCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	input, output := []byte(`{"request":1}`), []byte(`{"routes":[]}`)
	kind, _, err := j.resolve("key", input)
	if err != nil || kind != "first" {
		t.Fatalf("first resolve kind=%q err=%v", kind, err)
	}
	kind, _, err = j.resolve("key", input)
	if err != nil || kind != "mapping_unresolved" {
		t.Fatalf("reserved resolve kind=%q err=%v", kind, err)
	}
	if err := j.complete("key", input, output); err != nil {
		t.Fatal(err)
	}
	kind, replay, err := j.resolve("key", input)
	if err != nil || kind != "replay" || string(replay) != string(output) {
		t.Fatalf("replay kind=%q output=%s err=%v", kind, replay, err)
	}
}
