package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestOpenAIModelListURLs(t *testing.T) {
	got := openAIModelListURLs("https://example.com")
	want := []string{"https://example.com/models", "https://example.com/v1/models", "https://example.com/api/v1/models", "https://example.com/v1beta/models"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if got = openAIModelListURLs("https://example.com/v1/"); len(got) != 1 || got[0] != "https://example.com/v1/models" {
		t.Fatalf("versioned base candidates: %v", got)
	}
}

func TestFetchModelsFallsBackFromRootToV1(t *testing.T) {
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	models, err := FetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	})
	if err != nil || len(models) != 1 || models[0] != "gpt-4o" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if len(hits) < 2 || hits[0] != "/models" || hits[1] != "/v1/models" {
		t.Fatalf("probe order: %v", hits)
	}
}

func TestFetchModelsUsesBrowserHeadersAndSummarizesHTMLError(t *testing.T) {
	observedUserAgent := ""
	observedAccept := ""
	observedAcceptLanguage := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUserAgent = r.Header.Get("User-Agent")
		observedAccept = r.Header.Get("Accept")
		observedAcceptLanguage = r.Header.Get("Accept-Language")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head><body>blocked</body></html>`))
	}))
	defer server.Close()

	_, err := FetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
	})
	if err == nil {
		t.Fatalf("expected FetchModels to fail")
	}
	if !strings.Contains(err.Error(), "http 403: Just a moment...") {
		t.Fatalf("expected summarized HTML error, got %v", err)
	}
	if !strings.Contains(observedUserAgent, "Mozilla/5.0") {
		t.Fatalf("expected browser user-agent, got %q", observedUserAgent)
	}
	if observedAccept == "" {
		t.Fatalf("expected Accept header to be set")
	}
	if observedAcceptLanguage == "" {
		t.Fatalf("expected Accept-Language header to be set")
	}
}

func TestFetchGeminiModelsRejectsRepeatedPageToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-test"}],"nextPageToken":"repeat"}`))
	}))
	defer server.Close()

	_, err := fetchGeminiModels(server.Client(), context.Background(), model.Channel{
		Type:     outbound.OutboundTypeGemini,
		BaseUrls: []model.BaseUrl{{URL: server.URL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	})
	if err == nil || !strings.Contains(err.Error(), "repeated page token") {
		t.Fatalf("expected repeated page token error, got %v", err)
	}
}

func TestFetchAnthropicModelsRejectsRepeatedLastID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-test"}],"has_more":true,"last_id":"repeat"}`))
	}))
	defer server.Close()

	_, err := fetchAnthropicModels(server.Client(), context.Background(), model.Channel{
		Type:     outbound.OutboundTypeAnthropic,
		BaseUrls: []model.BaseUrl{{URL: server.URL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	})
	if err == nil || !strings.Contains(err.Error(), "repeated last_id") {
		t.Fatalf("expected repeated last_id error, got %v", err)
	}
}

type modelFetchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f modelFetchRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchAnthropicModelsUsesSenseNovaBearerAuth(t *testing.T) {
	var authorization string
	var apiKey string
	client := &http.Client{Transport: modelFetchRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		authorization = req.Header.Get("Authorization")
		apiKey = req.Header.Get("X-API-Key")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"sensenova-6.8-flash-lite"}],"has_more":false}`)),
			Request:    req,
		}, nil
	})}

	models, err := fetchAnthropicModels(client, context.Background(), model.Channel{
		Type:     outbound.OutboundTypeAnthropic,
		BaseUrls: []model.BaseUrl{{URL: "https://token.sensenova.cn/v1"}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sense-model-key"}},
	})
	if err != nil {
		t.Fatalf("fetchAnthropicModels() error = %v", err)
	}
	if len(models) != 1 || models[0] != "sensenova-6.8-flash-lite" {
		t.Fatalf("models = %v", models)
	}
	if authorization != "Bearer sense-model-key" {
		t.Fatalf("Authorization = %q", authorization)
	}
	if apiKey != "" {
		t.Fatalf("X-API-Key should be empty, got %q", apiKey)
	}
}

func TestModelFetchAccumulatorBoundsNames(t *testing.T) {
	accumulator := newModelFetchAccumulator(1)
	if err := accumulator.Add(strings.Repeat("x", maxFetchedModelNameBytes+1)); err == nil {
		t.Fatal("expected oversized model name to be rejected")
	}
}
