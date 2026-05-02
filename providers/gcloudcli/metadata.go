package gcloudcli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	metadataTokenPath   = "/computeMetadata/v1/instance/service-accounts/default/token"
	metadataEmailPath   = "/computeMetadata/v1/instance/service-accounts/default/email"
	metadataScopesPath  = "/computeMetadata/v1/instance/service-accounts/default/scopes"
	metadataProjectPath = "/computeMetadata/v1/project/project-id"
	metadataFlavor      = "Google"
)

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// metadataHandler returns an http.Handler that emulates the GCE metadata server.
// tokenFilePath, if non-empty, is a host-side path that receives each fresh token
// so that gcloud CLI's auth/access_token_file stays current.
func metadataHandler(account, serviceAccount, project, tokenFilePath string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(metadataTokenPath, func(w http.ResponseWriter, r *http.Request) {
		if !checkFlavor(w, r) {
			return
		}
		token, err := gcpPrintAccessToken(r.Context(), account, serviceAccount)
		if err != nil {
			http.Error(w, "token fetch failed", http.StatusInternalServerError)
			return
		}
		if tokenFilePath != "" {
			_ = os.WriteFile(tokenFilePath, []byte(token), 0o600)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tokenResponse{ //nolint:errcheck
			AccessToken: token,
			ExpiresIn:   1800,
			TokenType:   "Bearer",
		})
	})
	mux.HandleFunc(metadataEmailPath, func(w http.ResponseWriter, r *http.Request) {
		if !checkFlavor(w, r) {
			return
		}
		email := serviceAccount
		if email == "" {
			email = account
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(email)) //nolint:errcheck
	})
	mux.HandleFunc(metadataScopesPath, func(w http.ResponseWriter, r *http.Request) {
		if !checkFlavor(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("https://www.googleapis.com/auth/cloud-platform")) //nolint:errcheck
	})
	mux.HandleFunc(metadataProjectPath, func(w http.ResponseWriter, r *http.Request) {
		if !checkFlavor(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(project)) //nolint:errcheck
	})
	return mux
}

func checkFlavor(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Metadata-Flavor") != metadataFlavor {
		http.Error(w, "Missing or invalid Metadata-Flavor header", http.StatusForbidden)
		return false
	}
	return true
}

func gcpPrintAccessToken(ctx context.Context, account, serviceAccount string) (string, error) {
	args := []string{"auth", "print-access-token"}
	if account != "" {
		args = append(args, "--account="+account)
	}
	if serviceAccount != "" {
		args = append(args, "--impersonate-service-account="+serviceAccount)
	}
	out, err := exec.CommandContext(ctx, "gcloud", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
