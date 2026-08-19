package grouphealth

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestBuildProbeRequestForResponses(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "gpt-5.4")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/responses" {
		t.Fatalf("expected /v1/responses, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestForEmbeddings(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestForRerank(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeRerank,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sf-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "Pro/BAAI/bge-reranker-v2-m3")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/rerank" {
		t.Fatalf("expected /v1/rerank, got %s", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sf-test" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read probe body: %v", err)
	}
	var payload struct {
		Model     string   `json:"model"`
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode probe body: %v", err)
	}
	if payload.Model != "Pro/BAAI/bge-reranker-v2-m3" || payload.Query != "ping" || len(payload.Documents) != 1 || payload.Documents[0] != "ping" {
		t.Fatalf("unexpected rerank probe payload: %+v", payload)
	}
}
