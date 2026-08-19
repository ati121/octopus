package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestDetectPlatformRecognizesSiliconFlowURL(t *testing.T) {
	platform, routeType, err := DetectPlatform(context.Background(), "https://api.siliconflow.cn/v1/")
	if err != nil {
		t.Fatalf("DetectPlatform returned error: %v", err)
	}
	if platform != model.SitePlatformSiliconFlow {
		t.Fatalf("expected SiliconFlow platform, got %q", platform)
	}
	if routeType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected OpenAI chat default route, got %q", routeType)
	}
}

func TestSyncOfficialSiliconFlowUsesV1ModelsAndVerbatimKey(t *testing.T) {
	observedAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			http.NotFound(w, r)
		case "/v1/models":
			observedAuthorization = r.Header.Get("Authorization")
			if observedAuthorization != "Bearer sf-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-ai/DeepSeek-V3"},{"id":"Pro/BAAI/bge-m3"},{"id":"Pro/BAAI/bge-reranker-v2-m3"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncOfficialPlatform(context.Background(), &model.Site{
		Platform: model.SitePlatformSiliconFlow,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		Name:           "siliconflow-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "sf-secret",
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("syncOfficialPlatform returned error: %v", err)
	}
	if observedAuthorization != "Bearer sf-secret" {
		t.Fatalf("expected verbatim SiliconFlow key, got %q", observedAuthorization)
	}

	routes := make(map[string]model.SiteModelRouteType)
	for _, item := range snapshot.models {
		routes[item.ModelName] = item.RouteType
	}
	if routes["deepseek-ai/DeepSeek-V3"] != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected chat route, got %q", routes["deepseek-ai/DeepSeek-V3"])
	}
	if routes["Pro/BAAI/bge-m3"] != model.SiteModelRouteTypeOpenAIEmbedding {
		t.Fatalf("expected embedding route, got %q", routes["Pro/BAAI/bge-m3"])
	}
	if routes["Pro/BAAI/bge-reranker-v2-m3"] != model.SiteModelRouteTypeRerank {
		t.Fatalf("expected rerank route, got %q", routes["Pro/BAAI/bge-reranker-v2-m3"])
	}
}
