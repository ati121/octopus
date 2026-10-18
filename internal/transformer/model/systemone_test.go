package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/looplj/axonhub/llm"
)

func systemOneTestNativeRequest() *llm.Request {
	return &llm.Request{
		Model:       "jev-latest",
		RequestType: llm.RequestTypeSystemOne,
		APIFormat:   llm.APIFormatTypeSafeSystemOne,
		SystemOne: &llm.SystemOneRequest{
			State: "s",
			Questions: map[string]llm.SystemOneQuestion{
				"q": {Type: "noul", Instructions: "i"},
			},
		},
	}
}

func TestInternalLLMRequestValidateSupportsSystemOne(t *testing.T) {
	request := &InternalLLMRequest{
		Model:        "jev-1.13.0",
		RawAPIFormat: APIFormatSystemOne,
		Native:       systemOneTestNativeRequest(),
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

// System One 始终携带 AxonHub 原生请求，Validate 会直接交给库校验；
// 本地互斥表只覆盖不携带 Native 的 Rerank／Decisions 载荷路径。
func TestInternalLLMRequestSystemOneAlwaysUsesNativeValidation(t *testing.T) {
	request := &InternalLLMRequest{
		Model:    "jev-latest",
		Messages: []Message{{Role: "user"}},
		Native:   systemOneTestNativeRequest(),
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("expected native system one request to be validated by AxonHub, got %v", err)
	}

	// 没有 Native 时无法表示 System One 请求，本地校验会要求其它载荷。
	payloadless := &InternalLLMRequest{Model: "jev-latest"}
	if err := payloadless.Validate(); err == nil || !strings.Contains(err.Error(), "is required") {
		t.Fatalf("expected payload requirement error, got %v", err)
	}
	if payloadless.IsSystemOneRequest() {
		t.Fatal("expected system one detection to require the native AxonHub request")
	}
}

func TestInternalLLMResponseIsSystemOneResponse(t *testing.T) {
	response := &InternalLLMResponse{Native: &llm.Response{SystemOne: &llm.SystemOneResponse{}}}
	if !response.IsSystemOneResponse() {
		t.Fatal("expected system one response")
	}
	if (&InternalLLMResponse{}).IsSystemOneResponse() {
		t.Fatal("expected empty response not to be system one")
	}
}

func TestInternalLLMRequestValidateSupportsDecisions(t *testing.T) {
	request := &InternalLLMRequest{
		Model:            "gpt-6-luna",
		RawAPIFormat:     APIFormatOpenAIDecisions,
		DecisionsPayload: json.RawMessage(`{"model":"gpt-6-luna","input":"text","questions":[{"type":"choice"}]}`),
	}

	if err := request.Validate(); err != nil {
		t.Fatalf("expected decisions request to validate, got %v", err)
	}
	if !request.IsDecisionsRequest() {
		t.Fatal("expected IsDecisionsRequest to return true")
	}
	if request.IsChatRequest() || request.IsEmbeddingRequest() || request.IsRerankRequest() || request.IsSystemOneRequest() {
		t.Fatal("expected decisions request not to be classified as chat, embedding, rerank or system one")
	}
}

func TestInternalLLMRequestValidateKeepsDecisionsMutuallyExclusive(t *testing.T) {
	payload := json.RawMessage(`{"input":"text","questions":[{"type":"choice"}]}`)
	tests := []struct {
		name    string
		request InternalLLMRequest
	}{
		{
			name: "decisions and chat",
			request: InternalLLMRequest{
				Model:            "model",
				Messages:         []Message{{Role: "user"}},
				DecisionsPayload: payload,
			},
		},
		{
			name: "decisions and embedding",
			request: InternalLLMRequest{
				Model:            "model",
				EmbeddingInput:   &EmbeddingInput{Single: rerankTestStringPtr("embedding")},
				DecisionsPayload: payload,
			},
		},
		{
			name: "decisions and rerank",
			request: InternalLLMRequest{
				Model:            "model",
				RerankPayload:    json.RawMessage(`{"query":"q","documents":["d"]}`),
				DecisionsPayload: payload,
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

func TestInternalLLMRequestValidateRejectsInvalidDecisionsPayload(t *testing.T) {
	request := &InternalLLMRequest{
		Model:            "model",
		DecisionsPayload: json.RawMessage(`{`),
	}

	if err := request.Validate(); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected invalid decisions JSON error, got %v", err)
	}
}

func TestInternalLLMResponseIsDecisionsResponse(t *testing.T) {
	response := &InternalLLMResponse{DecisionsPayload: json.RawMessage(`{"answers":[]}`)}
	if !response.IsDecisionsResponse() {
		t.Fatal("expected decisions response")
	}
	if (&InternalLLMResponse{}).IsDecisionsResponse() {
		t.Fatal("expected empty response not to be decisions")
	}
}
