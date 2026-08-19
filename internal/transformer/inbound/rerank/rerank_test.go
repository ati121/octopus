package rerank

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformRequestPreservesPayloadAndValidatesRequiredFields(t *testing.T) {
	body := []byte(`{"model":"  Pro/BAAI/bge-reranker-v2-m3  ","query":"octopus","documents":["first","second"],"top_n":1,"return_documents":true,"vendor_extension":{"mode":"fast"}}`)

	request, err := (&Inbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if request.Model != "Pro/BAAI/bge-reranker-v2-m3" {
		t.Fatalf("expected trimmed model, got %q", request.Model)
	}
	if request.RawAPIFormat != model.APIFormatRerank {
		t.Fatalf("expected rerank API format, got %q", request.RawAPIFormat)
	}
	if !bytes.Equal(request.RerankPayload, body) {
		t.Fatalf("expected raw rerank payload to be preserved, got %s", request.RerankPayload)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("expected transformed request to validate, got %v", err)
	}
}

func TestTransformRequestAcceptsStructuredQuery(t *testing.T) {
	body := []byte(`{"model":"multimodal-reranker","query":{"text":"octopus"},"documents":[{"text":"first"}]}`)

	request, err := (&Inbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if !request.IsRerankRequest() {
		t.Fatal("expected rerank request")
	}
}

func TestTransformRequestRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{name: "invalid json", body: `{"model":`, message: "invalid rerank request"},
		{name: "missing model", body: `{"query":"q","documents":["d"]}`, message: "model is required"},
		{name: "blank model", body: `{"model":"  ","query":"q","documents":["d"]}`, message: "model is required"},
		{name: "missing query", body: `{"model":"m","documents":["d"]}`, message: "query is required"},
		{name: "blank query", body: `{"model":"m","query":"  ","documents":["d"]}`, message: "query is required"},
		{name: "empty query object", body: `{"model":"m","query":{},"documents":["d"]}`, message: "query is required"},
		{name: "missing documents", body: `{"model":"m","query":"q"}`, message: "documents are required"},
		{name: "null documents", body: `{"model":"m","query":"q","documents":null}`, message: "documents are required"},
		{name: "empty documents", body: `{"model":"m","query":"q","documents":[]}`, message: "documents are required"},
		{name: "blank document string", body: `{"model":"m","query":"q","documents":"  "}`, message: "documents are required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (&Inbound{}).TransformRequest(context.Background(), []byte(tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected error containing %q, got %v", tt.message, err)
			}
		})
	}
}

func TestTransformResponseReturnsProviderPayloadUnchanged(t *testing.T) {
	raw := []byte(`{"id":"rerank-1","results":[{"index":0,"relevance_score":0.99}],"meta":{"tokens":{"input_tokens":3}},"extension":{"trace_id":"abc"}}`)
	response := &model.InternalLLMResponse{RerankPayload: raw}
	inbound := &Inbound{}

	body, err := inbound.TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if !bytes.Equal(body, raw) {
		t.Fatalf("expected response payload to be preserved, got %s", body)
	}
	stored, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse returned error: %v", err)
	}
	if stored != response {
		t.Fatal("expected inbound adapter to retain the internal response")
	}

	body[0] = '['
	if response.RerankPayload[0] != '{' {
		t.Fatal("expected returned response bytes to be a copy")
	}
}

func TestTransformResponseRejectsEmptyResponse(t *testing.T) {
	for _, response := range []*model.InternalLLMResponse{nil, {}} {
		if _, err := (&Inbound{}).TransformResponse(context.Background(), response); err == nil {
			t.Fatal("expected empty rerank response to be rejected")
		}
	}
}
