package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInternalLLMRequestValidateSupportsSystemOne(t *testing.T) {
	request := &InternalLLMRequest{
		Model:            "jev-1.13.0",
		RawAPIFormat:     APIFormatSystemOne,
		SystemOnePayload: json.RawMessage(`{"model":"jev-latest","state":"s","questions":{"q":{"type":"noul","instructions":"i"}}}`),
	}

	if err := request.Validate(); err != nil {
		t.Fatalf("expected system one request to validate, got %v", err)
	}
	if !request.IsSystemOneRequest() {
		t.Fatal("expected IsSystemOneRequest to return true")
	}
	if request.IsChatRequest() || request.IsEmbeddingRequest() || request.IsRerankRequest() {
		t.Fatal("expected system one request not to be classified as chat, embedding or rerank")
	}
}

func TestInternalLLMRequestValidateKeepsSystemOneMutuallyExclusive(t *testing.T) {
	payload := json.RawMessage(`{"state":"s","questions":{"q":{"type":"noul","instructions":"i"}}}`)
	tests := []struct {
		name    string
		request InternalLLMRequest
	}{
		{
			name: "system one and chat",
			request: InternalLLMRequest{
				Model:            "model",
				Messages:         []Message{{Role: "user"}},
				SystemOnePayload: payload,
			},
		},
		{
			name: "system one and embedding",
			request: InternalLLMRequest{
				Model:            "model",
				EmbeddingInput:   &EmbeddingInput{Single: rerankTestStringPtr("embedding")},
				SystemOnePayload: payload,
			},
		},
		{
			name: "system one and rerank",
			request: InternalLLMRequest{
				Model:            "model",
				RerankPayload:    json.RawMessage(`{"query":"q","documents":["d"]}`),
				SystemOnePayload: payload,
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

func TestInternalLLMRequestValidateRejectsInvalidSystemOnePayload(t *testing.T) {
	request := &InternalLLMRequest{
		Model:            "model",
		SystemOnePayload: json.RawMessage(`{`),
	}

	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected invalid system one JSON error, got %v", err)
	}
}

func TestInternalLLMResponseIsSystemOneResponse(t *testing.T) {
	response := &InternalLLMResponse{SystemOnePayload: json.RawMessage(`{"answers":{}}`)}
	if !response.IsSystemOneResponse() {
		t.Fatal("expected system one response")
	}
	if (&InternalLLMResponse{}).IsSystemOneResponse() {
		t.Fatal("expected empty response not to be system one")
	}
}
