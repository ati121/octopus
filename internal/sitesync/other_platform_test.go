package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestDetectPlatformFallsBackToOther(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>SenseNova Token</title></head><body>商汤大装置</body></html>`))
		case "/api/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"service":"sensenova"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platform, routeType, err := DetectPlatform(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DetectPlatform returned error: %v", err)
	}
	if platform != model.SitePlatformOther {
		t.Fatalf("expected Other platform, got %q", platform)
	}
	if routeType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected OpenAI chat default route, got %q", routeType)
	}
}

func TestSyncOtherPlatformUsesDirectV1ModelsAndVerbatimKey(t *testing.T) {
	observedAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			http.NotFound(w, r)
		case "/v1/models":
			observedAuthorization = r.Header.Get("Authorization")
			if observedAuthorization != "Bearer sensenova-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"SenseChat-5"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	siteRecord := &model.Site{
		Platform:         model.SitePlatformOther,
		BaseURL:          server.URL,
		DefaultRouteType: model.SiteModelRouteTypeOpenAIChat,
	}
	account := &model.SiteAccount{
		Name:           "sensenova-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "sensenova-secret",
		Enabled:        true,
	}

	snapshot, err := syncAccountState(context.Background(), siteRecord, account)
	if err != nil {
		t.Fatalf("syncAccountState returned error: %v", err)
	}
	if observedAuthorization != "Bearer sensenova-secret" {
		t.Fatalf("expected verbatim Other-platform key, got %q", observedAuthorization)
	}
	if len(snapshot.models) != 1 || snapshot.models[0].ModelName != "SenseChat-5" {
		t.Fatalf("unexpected synced models: %#v", snapshot.models)
	}

	checkin, _, err := checkinAccountState(context.Background(), siteRecord, account)
	if err != nil {
		t.Fatalf("checkinAccountState returned error: %v", err)
	}
	if checkin.Status != model.SiteExecutionStatusSkipped {
		t.Fatalf("expected Other-platform check-in to be skipped, got %#v", checkin)
	}
}
