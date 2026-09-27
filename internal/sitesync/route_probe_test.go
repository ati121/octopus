package sitesync

import (
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestPickPreferredDetectedRouteType(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		values    []model.SiteModelRouteType
		expected  model.SiteModelRouteType
	}{
		{
			name:      "claude prefers anthropic when available",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeAnthropic},
			expected:  model.SiteModelRouteTypeAnthropic,
		},
		{
			name:      "claude falls back to response before chat",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeOpenAIResponse},
			expected:  model.SiteModelRouteTypeOpenAIResponse,
		},
		{
			name:      "claude falls back to chat before gemini when anthropic missing",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeGemini, model.SiteModelRouteTypeOpenAIChat},
			expected:  model.SiteModelRouteTypeOpenAIChat,
		},
		{
			name:      "gemini keeps native route when available",
			modelName: "gemini-2.0-flash",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeGemini},
			expected:  model.SiteModelRouteTypeGemini,
		},
		{
			name:      "gpt prefers response over chat",
			modelName: "gpt-4o-mini",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeOpenAIResponse},
			expected:  model.SiteModelRouteTypeOpenAIResponse,
		},
		{
			name:      "reranker prefers rerank when available",
			modelName: "Pro/BAAI/bge-reranker-v2-m3",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeRerank},
			expected:  model.SiteModelRouteTypeRerank,
		},
		{
			name:      "embedding bge prefers embedding when available",
			modelName: "Pro/BAAI/bge-m3",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeOpenAIEmbedding},
			expected:  model.SiteModelRouteTypeOpenAIEmbedding,
		},
		{
			name:      "jev prefers system one over chat",
			modelName: "jev-1.13.0",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeSystemOne},
			expected:  model.SiteModelRouteTypeSystemOne,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := pickPreferredDetectedRouteType(tt.modelName, tt.values); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestBuildSiteModelRouteDetectionAddsHeuristicResponsesForGPT5(t *testing.T) {
	detection, ok := buildSiteModelRouteDetection(
		"gpt-5.4",
		nil,
		[]string{"/v1/chat/completions"},
		"/api/pricing",
		map[string]struct{}{"gpt-5.4": {}},
	)
	if !ok {
		t.Fatalf("expected heuristic response detection to be produced")
	}

	metadata, ok := model.ParseSiteModelRouteMetadata(detection.RouteRawPayload)
	if !ok {
		t.Fatalf("expected route metadata to parse")
	}
	if metadata.RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected heuristic detection route type %q, got %q", model.SiteModelRouteTypeOpenAIResponse, metadata.RouteType)
	}
	if len(metadata.SupportedEndpointTypes) != 1 || metadata.SupportedEndpointTypes[0] != "/v1/chat/completions" {
		t.Fatalf("expected upstream endpoint list to remain intact, got %#v", metadata.SupportedEndpointTypes)
	}
	if len(metadata.HeuristicEndpointTypes) != 1 || metadata.HeuristicEndpointTypes[0] != "/v1/responses" {
		t.Fatalf("expected heuristic endpoint list to record injected response support, got %#v", metadata.HeuristicEndpointTypes)
	}
	if len(metadata.NormalizedEndpointTypes) != 2 {
		t.Fatalf("expected normalized endpoint list to include explicit and heuristic routes, got %#v", metadata.NormalizedEndpointTypes)
	}
}

func TestBuildSiteModelRouteDetectionGuessesRouteFromModelName(t *testing.T) {
	tests := []struct {
		name                   string
		modelName              string
		enableGroups           []string
		supportedEndpointTypes []string
		expected               model.SiteModelRouteType
	}{
		{
			name:                   "unmappable endpoint types fall back to embedding guess",
			modelName:              "vendor-embedding-x",
			supportedEndpointTypes: []string{"/vendor/embeddings"},
			expected:               model.SiteModelRouteTypeOpenAIEmbedding,
		},
		{
			name:                   "unmappable endpoint types fall back to rerank guess",
			modelName:              "Pro/BAAI/bge-reranker-v2-m3",
			supportedEndpointTypes: []string{"/vendor/custom"},
			expected:               model.SiteModelRouteTypeRerank,
		},
		{
			name:                   "unmappable endpoint types fall back to anthropic guess",
			modelName:              "claude-3-5-sonnet",
			supportedEndpointTypes: []string{"/vendor/custom"},
			expected:               model.SiteModelRouteTypeAnthropic,
		},
		{
			name:         "enable groups without endpoint types fall back to chat guess",
			modelName:    "vendor-chat-x",
			enableGroups: []string{"default"},
			expected:     model.SiteModelRouteTypeOpenAIChat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detection, ok := buildSiteModelRouteDetection(
				tt.modelName,
				tt.enableGroups,
				tt.supportedEndpointTypes,
				"/api/pricing",
				map[string]struct{}{strings.ToLower(tt.modelName): {}},
			)
			if !ok {
				t.Fatalf("expected guessed detection to be produced")
			}
			if detection.RouteType != tt.expected {
				t.Fatalf("expected guessed route type %q, got %q", tt.expected, detection.RouteType)
			}

			metadata, ok := model.ParseSiteModelRouteMetadata(detection.RouteRawPayload)
			if !ok {
				t.Fatalf("expected route metadata to parse")
			}
			if !metadata.RouteSupported {
				t.Fatalf("expected guessed metadata to mark route as supported")
			}
			if !metadata.RouteGuessed {
				t.Fatalf("expected guessed metadata to record name guess")
			}
			if metadata.RouteType != tt.expected {
				t.Fatalf("expected guessed metadata route type %q, got %q", tt.expected, metadata.RouteType)
			}
		})
	}
}

func TestMapSupportedEndpointTypeRecognizesRerank(t *testing.T) {
	for _, value := range []string{"rerank", "reranker", "/v1/rerank"} {
		routeType, ok := mapSupportedEndpointType(value)
		if !ok || routeType != model.SiteModelRouteTypeRerank {
			t.Fatalf("expected %q to map to rerank, got %q ok=%v", value, routeType, ok)
		}
	}
}

func TestMapSupportedEndpointTypeRecognizesSystemOne(t *testing.T) {
	for _, value := range []string{"systemone", "system_one", "system-one", "/v1/systemone"} {
		routeType, ok := mapSupportedEndpointType(value)
		if !ok || routeType != model.SiteModelRouteTypeSystemOne {
			t.Fatalf("expected %q to map to system one, got %q ok=%v", value, routeType, ok)
		}
	}
}

func TestBuildSiteModelRouteDetectionAddsHeuristicSystemOneForJev(t *testing.T) {
	tests := []struct {
		name                   string
		modelName              string
		supportedEndpointTypes []string
		expectedHeuristic      []string
	}{
		{
			// 站点对不认识的模型只报告默认的 openai 端点，旧逻辑据此把 jev 判成 Chat。
			name:                   "default openai endpoint does not win over system one",
			modelName:              "jev-1.13.0",
			supportedEndpointTypes: []string{"openai"},
			expectedHeuristic:      []string{"/v1/systemone"},
		},
		{
			name:              "missing endpoint types still detect system one",
			modelName:         "jev-latest",
			expectedHeuristic: []string{"/v1/systemone"},
		},
		{
			name:                   "explicit system one endpoint needs no heuristic",
			modelName:              "jev-latest",
			supportedEndpointTypes: []string{"/v1/systemone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detection, ok := buildSiteModelRouteDetection(
				tt.modelName,
				nil,
				tt.supportedEndpointTypes,
				"/api/pricing",
				map[string]struct{}{tt.modelName: {}},
			)
			if !ok {
				t.Fatalf("expected system one detection to be produced")
			}
			if detection.RouteType != model.SiteModelRouteTypeSystemOne {
				t.Fatalf("expected route type %q, got %q", model.SiteModelRouteTypeSystemOne, detection.RouteType)
			}

			metadata, ok := model.ParseSiteModelRouteMetadata(detection.RouteRawPayload)
			if !ok {
				t.Fatalf("expected route metadata to parse")
			}
			if metadata.RouteGuessed {
				t.Fatalf("expected system one detection not to be marked as a name guess")
			}
			if strings.Join(metadata.HeuristicEndpointTypes, ",") != strings.Join(tt.expectedHeuristic, ",") {
				t.Fatalf("expected heuristic endpoint types %#v, got %#v", tt.expectedHeuristic, metadata.HeuristicEndpointTypes)
			}
		})
	}
}

func TestMergeSiteModelRouteDetectionsPrefersDetectedOverGuessed(t *testing.T) {
	guessed, ok := buildSiteModelRouteDetection(
		"vendor-chat-x",
		[]string{"default"},
		nil,
		"/api/pricing",
		nil,
	)
	if !ok {
		t.Fatalf("expected guessed detection to be produced")
	}
	detected, ok := buildSiteModelRouteDetection(
		"vendor-chat-x",
		nil,
		[]string{"/v1/messages"},
		"/api/available_model",
		nil,
	)
	if !ok {
		t.Fatalf("expected explicit detection to be produced")
	}

	merged := mergeSiteModelRouteDetections(
		map[string]siteModelRouteDetection{"vendor-chat-x": guessed},
		map[string]siteModelRouteDetection{"vendor-chat-x": detected},
	)
	if merged["vendor-chat-x"].RouteType != model.SiteModelRouteTypeAnthropic {
		t.Fatalf("expected explicit detection to replace guessed detection, got %q", merged["vendor-chat-x"].RouteType)
	}

	merged = mergeSiteModelRouteDetections(
		map[string]siteModelRouteDetection{"vendor-chat-x": detected},
		map[string]siteModelRouteDetection{"vendor-chat-x": guessed},
	)
	if merged["vendor-chat-x"].RouteType != model.SiteModelRouteTypeAnthropic {
		t.Fatalf("expected guessed detection to not replace explicit detection, got %q", merged["vendor-chat-x"].RouteType)
	}
}
