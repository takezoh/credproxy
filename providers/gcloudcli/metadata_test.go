package gcloudcli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func stubGcloudForMetadata(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestMetadataHandler_tokenEndpoint_missingFlavor(t *testing.T) {
	h := metadataHandler("user@example.com", "", "proj-x", "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, metadataTokenPath, nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestMetadataHandler_tokenEndpoint_returnsToken(t *testing.T) {
	stubGcloudForMetadata(t, "test-access-token-xyz")
	h := metadataHandler("user@example.com", "", "proj-x", "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, metadataTokenPath, nil)
	r.Header.Set("Metadata-Flavor", "Google")
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp tokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "test-access-token-xyz" {
		t.Errorf("access_token = %q, want %q", resp.AccessToken, "test-access-token-xyz")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want > 0", resp.ExpiresIn)
	}
}

func TestMetadataHandler_emailEndpoint(t *testing.T) {
	h := metadataHandler("user@example.com", "sa@proj.iam.gserviceaccount.com", "proj-x", "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, metadataEmailPath, nil)
	r.Header.Set("Metadata-Flavor", "Google")
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if body := w.Body.String(); body != "sa@proj.iam.gserviceaccount.com" {
		t.Errorf("email = %q, want SA email", body)
	}
}

func TestMetadataHandler_projectEndpoint(t *testing.T) {
	h := metadataHandler("user@example.com", "", "my-project-123", "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, metadataProjectPath, nil)
	r.Header.Set("Metadata-Flavor", "Google")
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if body := w.Body.String(); body != "my-project-123" {
		t.Errorf("project = %q, want %q", body, "my-project-123")
	}
}

func TestMetadataHandler_tokenEndpoint_impersonatesSA(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --impersonate-service-account=*) echo "sa-token-abc"; exit 0 ;;
  esac
done
echo "user-token"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	h := metadataHandler("user@example.com", "sa@proj.iam.gserviceaccount.com", "proj-x", "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, metadataTokenPath, nil)
	r.Header.Set("Metadata-Flavor", "Google")
	h.ServeHTTP(w, r)

	var resp tokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "sa-token-abc" {
		t.Errorf("access_token = %q, want sa-token-abc", resp.AccessToken)
	}
}
