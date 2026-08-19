package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInternalLLMRequestValidateSupportsRerank(t *testing.T) {
	request := &InternalLLMRequest{
		Model:         "Pro/BAAI/bge-reranker-v2-m3",
		RawAPIFormat:  APIFormatRerank,
		RerankPayload: json.RawMessage(`{"model":"alias","query":"q","documents":["d"]}`),
	}

	if err := request.Validate(); err != nil {
		t.Fatalf("expected rerank request to validate, got %v", err)
	}
	if !request.IsRerankRequest() {
		t.Fatal("expected IsRerankRequest to return true")
	}
}

func TestInternalLLMRequestValidateKeepsRequestTypesMutuallyExclusive(t *testing.T) {
	embeddingInput := &EmbeddingInput{Single: rerankTestStringPtr("embedding")}
	tests := []struct {
		name    string
		request InternalLLMRequest
	}{
		{
			name: "rerank and chat",
			request: InternalLLMRequest{
				Model:         "model",
				Messages:      []Message{{Role: "user"}},
				RerankPayload: json.RawMessage(`{"query":"q","documents":["d"]}`),
			},
		},
		{
			name: "rerank and embedding",
			request: InternalLLMRequest{
				Model:          "model",
				EmbeddingInput: embeddingInput,
				RerankPayload:  json.RawMessage(`{"query":"q","documents":["d"]}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("expected mutually exclusive validation error, got %v", err)
			}
		})
	}
}

func TestInternalLLMRequestValidateRejectsInvalidRerankPayload(t *testing.T) {
	request := &InternalLLMRequest{
		Model:         "model",
		RerankPayload: json.RawMessage(`{`),
	}

	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected invalid rerank JSON error, got %v", err)
	}
}

func TestInternalLLMResponseIsRerankResponse(t *testing.T) {
	response := &InternalLLMResponse{RerankPayload: json.RawMessage(`{"results":[]}`)}
	if !response.IsRerankResponse() {
		t.Fatal("expected rerank response")
	}
	if (&InternalLLMResponse{}).IsRerankResponse() {
		t.Fatal("expected empty response not to be rerank")
	}
}

func rerankTestStringPtr(value string) *string {
	return &value
}
