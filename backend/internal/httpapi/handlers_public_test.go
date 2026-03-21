package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"linuxdospace/backend/internal/config"
)

// TestHandleHealthIncludesStartupWarnings verifies that startup warnings are
// surfaced through `/healthz` instead of being hidden only in process logs.
func TestHandleHealthIncludesStartupWarnings(t *testing.T) {
	api := &API{
		config: config.Config{
			App: config.AppConfig{
				Name: "LinuxDoSpace",
				Env:  "production",
			},
			LinuxDO: config.LinuxDOConfig{
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				RedirectURL:  "https://api.example.com/v1/auth/callback",
			},
			Cloudflare: config.CloudflareConfig{
				APIToken: "test-token",
			},
		},
		version:         "test-revision",
		startupWarnings: []string{"default mail root linuxdo.space still uses Cloudflare-managed Email Routing MX records"},
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	api.handleHealth(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 health response, got %d", recorder.Code)
	}

	var response struct {
		Data struct {
			Status          string   `json:"status"`
			Degraded        bool     `json:"degraded"`
			StartupWarnings []string `json:"startup_warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if response.Data.Status != "ok" {
		t.Fatalf("expected status ok, got %q", response.Data.Status)
	}
	if !response.Data.Degraded {
		t.Fatalf("expected degraded=true when startup warnings exist")
	}
	if len(response.Data.StartupWarnings) != 1 {
		t.Fatalf("expected one startup warning, got %+v", response.Data.StartupWarnings)
	}
}
