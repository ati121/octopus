package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformRequestReplacesModelAndPreservesExtensions(t *testing.T) {
	raw := json.RawMessage(`{"model":"client-alias","query":"octopus","documents":["first","second"],"top_n":1,"return_documents":true,"vendor_extension":{"mode":"fast"}}`)
	request := &model.InternalLLMRequest{
		Model:         "Pro/BAAI/bge-reranker-v2-m3",
		RawAPIFormat:  model.APIFormatRerank,
		RerankPayload: append(json.RawMessage(nil), raw...),
	}

	httpRequest, err := (&Outbound{}).TransformRequest(
		context.Background(),
		request,
		"https://api.siliconflow.cn/v1/",
		"sf-secret",
	)
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if httpRequest.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", httpRequest.Method)
	}
	if httpRequest.URL.String() != "https://api.siliconflow.cn/v1/rerank" {
		t.Fatalf("unexpected rerank URL: %s", httpRequest.URL)
	}
	if got := httpRequest.Header.Get("Authorization"); got != "Bearer sf-secret" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := httpRequest.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected Content-Type header: %q", got)
	}
	if got := httpRequest.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	body, err := io.ReadAll(httpRequest.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	var routedModel string
	if err := json.Unmarshal(payload["model"], &routedModel); err != nil {
		t.Fatalf("decode routed model: %v", err)
	}
	if routedModel != request.Model {
		t.Fatalf("expected routed model %q, got %q", request.Model, routedModel)
	}
	for _, field := range []string{"query", "documents", "top_n", "return_documents", "vendor_extension"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("expected extension field %q to be preserved in %s", field, body)
		}
	}
	if !bytes.Equal(request.RerankPayload, raw) {
		t.Fatalf("expected internal rerank payload to remain unchanged, got %s", request.RerankPayload)
	}
}

func TestTransformRequestRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		request *model.InternalLLMRequest
		baseURL string
	}{
		{name: "nil request", baseURL: "https://example.com/v1"},
		{name: "non rerank request", request: &model.InternalLLMRequest{Model: "m"}, baseURL: "https://example.com/v1"},
		{name: "invalid payload", request: &model.InternalLLMRequest{Model: "m", RerankPayload: json.RawMessage(`{`)}, baseURL: "https://example.com/v1"},
		{name: "non object payload", request: &model.InternalLLMRequest{Model: "m", RerankPayload: json.RawMessage(`null`)}, baseURL: "https://example.com/v1"},
		{name: "invalid base url", request: &model.InternalLLMRequest{Model: "m", RerankPayload: json.RawMessage(`{"model":"m","query":"q","documents":["d"]}`)}, baseURL: "/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&Outbound{}).TransformRequest(context.Background(), tt.request, tt.baseURL, "key"); err == nil {
				t.Fatal("expected TransformRequest to fail")
			}
		})
	}
}

func TestTransformResponsePreservesPayloadAndParsesTokenUsage(t *testing.T) {
	raw := []byte(`{"id":"rerank-1","results":[{"index":1,"relevance_score":0.98,"document":{"text":"second"}}],"meta":{"tokens":{"input_tokens":11,"output_tokens":2,"image_tokens":3}},"extension":{"trace_id":"abc"}}`)
	response := &http.Response{Body: io.NopCloser(bytes.NewReader(raw))}

	internalResponse, err := (&Outbound{}).TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if internalResponse.ID != "rerank-1" {
		t.Fatalf("expected response ID, got %q", internalResponse.ID)
	}
	if !bytes.Equal(internalResponse.RerankPayload, raw) {
		t.Fatalf("expected provider response to be preserved, got %s", internalResponse.RerankPayload)
	}
	if internalResponse.Usage == nil {
		t.Fatal("expected token usage")
	}
	if internalResponse.Usage.PromptTokens != 14 || internalResponse.Usage.CompletionTokens != 2 || internalResponse.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected token usage: %+v", internalResponse.Usage)
	}
}

func TestTransformResponseFallsBackToBilledUnits(t *testing.T) {
	raw := `{"results":[],"meta":{"billed_units":{"input_tokens":7,"output_tokens":1,"image_tokens":2}}}`
	response := &http.Response{Body: io.NopCloser(strings.NewReader(raw))}

	internalResponse, err := (&Outbound{}).TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if internalResponse.Usage == nil || internalResponse.Usage.PromptTokens != 9 || internalResponse.Usage.CompletionTokens != 1 || internalResponse.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected billed unit usage: %+v", internalResponse.Usage)
	}
}

func TestTransformResponseRejectsInvalidResults(t *testing.T) {
	tests := []string{
		`{}`,
		`{"results":null}`,
		`{"results":{}}`,
		`{"results":"not-an-array"}`,
	}

	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			response := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
			if _, err := (&Outbound{}).TransformResponse(context.Background(), response); err == nil {
				t.Fatalf("expected invalid results payload %s to fail", body)
			}
		})
	}
}
