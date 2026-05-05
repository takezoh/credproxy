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
	metadataTokenPath      = "/computeMetadata/v1/instance/service-accounts/default/token"
	metadataEmailPath      = "/computeMetadata/v1/instance/service-accounts/default/email"
	metadataScopesPath     = "/computeMetadata/v1/instance/service-accounts/default/scopes"
	metadataProjectPath    = "/computeMetadata/v1/project/project-id"
	metadataServiceAccPath = "/computeMetadata/v1/instance/service-accounts/default/"
	metadataFlavor         = "Google"
	metadataCloudPlatform  = "https://www.googleapis.com/auth/cloud-platform"
)

type serviceAccountInfo struct {
	Aliases []string `json:"aliases"`
	Email   string   `json:"email"`
	Scopes  []string `json:"scopes"`
}

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
	mux.HandleFunc("/", serveMetadataDir("/", "0.1/\ncomputeMetadata/\n"))
	mux.HandleFunc("/computeMetadata/", serveMetadataDir("/computeMetadata/", "v1/\n"))
	mux.HandleFunc("/computeMetadata/v1/", serveMetadataDir("/computeMetadata/v1/", "instance/\nproject/\n"))
	mux.HandleFunc(metadataServiceAccPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != metadataServiceAccPath {
			http.NotFound(w, r)
			return
		}
		if !checkFlavor(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(serviceAccountInfo{ //nolint:errcheck
			Aliases: []string{"default"},
			Email:   resolveEmail(serviceAccount, account),
			Scopes:  []string{metadataCloudPlatform},
		})
	})
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
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(resolveEmail(serviceAccount, account))) //nolint:errcheck
	})
	mux.HandleFunc(metadataScopesPath, func(w http.ResponseWriter, r *http.Request) {
		if !checkFlavor(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(metadataCloudPlatform)) //nolint:errcheck
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

func serveMetadataDir(path, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		if !checkFlavor(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/text")
		w.Write([]byte(body)) //nolint:errcheck
	}
}

func resolveEmail(serviceAccount, account string) string {
	if serviceAccount != "" {
		return serviceAccount
	}
	return account
}

func checkFlavor(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Metadata-Flavor") != metadataFlavor {
		http.Error(w, "Missing or invalid Metadata-Flavor header", http.StatusForbidden)
		return false
	}
	w.Header().Set("Metadata-Flavor", metadataFlavor)
	return true
}

func gcpPrintAccessToken(ctx context.Context, account, serviceAccount string) (string, error) {
	args := buildPrintAccessTokenArgs(account, serviceAccount)
	out, err := exec.CommandContext(ctx, "gcloud", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// buildPrintAccessTokenArgs returns gcloud args for printing an access token.
// SA mode uses print-access-token + impersonation; user-account mode uses
// application-default print-access-token to obtain an ADC token (audience =
// Google auth library client) accepted by APIs such as Cloud Run RunJob.
func buildPrintAccessTokenArgs(account, serviceAccount string) []string {
	if serviceAccount != "" {
		args := []string{"auth", "print-access-token"}
		if account != "" {
			args = append(args, "--account="+account)
		}
		args = append(args, "--impersonate-service-account="+serviceAccount)
		return args
	}
	return []string{"auth", "application-default", "print-access-token"}
}
