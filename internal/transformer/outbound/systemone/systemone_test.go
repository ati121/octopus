package systemone

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

func TestTransformRequestReplacesModelAndPreservesPayload(t *testing.T) {
	raw := json.RawMessage(`{"model":"client-alias","state":{"subject":"Duplicate charge","message":"Please help."},"questions":{"department":{"type":"choice","instructions":"Which team?","criteria":{"billing":"Payments","technical":null}}},"vendor_extension":{"mode":"fast"}}`)
	request := &model.InternalLLMRequest{
		Model:            "jev-1.13.0",
		RawAPIFormat:     model.APIFormatSystemOne,
		SystemOnePayload: append(json.RawMessage(nil), raw...),
	}

	httpRequest, err := (&Outbound{}).TransformRequest(
		context.Background(),
		request,
		"https://api.typesafe.ai/v1/",
		"ts-secret",
	)
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if httpRequest.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", httpRequest.Method)
	}
	if httpRequest.URL.String() != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("unexpected system one URL: %s", httpRequest.URL)
	}
	if got := httpRequest.Header.Get("Authorization"); got != "Bearer ts-secret" {
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
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatalf("decode original payload: %v", err)
	}
	for _, field := range []string{"state", "questions", "vendor_extension"} {
		if !jsonEqual(t, payload[field], original[field]) {
			t.Fatalf("expected field %q to be preserved, got %s", field, payload[field])
		}
	}
	if !bytes.Equal(request.SystemOnePayload, raw) {
		t.Fatalf("expected internal system one payload to remain unchanged, got %s", request.SystemOnePayload)
	}
}

func TestTransformRequestRejectsInvalidInput(t *testing.T) {
	validPayload := json.RawMessage(`{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":"i"}}}`)
	tests := []struct {
		name    string
		request *model.InternalLLMRequest
		baseURL string
	}{
		{name: "nil request", baseURL: "https://example.com/v1"},
		{name: "non system one request", request: &model.InternalLLMRequest{Model: "m", RerankPayload: json.RawMessage(`{"query":"q"}`)}, baseURL: "https://example.com/v1"},
		{name: "invalid payload", request: &model.InternalLLMRequest{Model: "m", SystemOnePayload: json.RawMessage(`{`)}, baseURL: "https://example.com/v1"},
		{name: "non object payload", request: &model.InternalLLMRequest{Model: "m", SystemOnePayload: json.RawMessage(`null`)}, baseURL: "https://example.com/v1"},
		{name: "invalid base url", request: &model.InternalLLMRequest{Model: "m", SystemOnePayload: validPayload}, baseURL: "/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&Outbound{}).TransformRequest(context.Background(), tt.request, tt.baseURL, "key"); err == nil {
				t.Fatal("expected TransformRequest to fail")
			}
		})
	}
}

func TestTransformResponsePreservesPayloadAndParsesModelAndUsage(t *testing.T) {
	raw := []byte(`{"model":"jev-1.13.0","answers":{"department":{"type":"choice","choice":"billing","probabilities":{"billing":0.88,"technical":0.12},"confidence":0.81},"frustration":{"type":"score","score":1.05,"legend":{"0":"Calm","1":"Frustrated"},"probabilities":{"0":0.05,"1":0.95},"confidence":0.92},"is_urgent":{"type":"noul","noul":0.95}},"usage":{"input_tokens":392,"output_tokens":65}}`)
	response := &http.Response{Body: io.NopCloser(bytes.NewReader(raw))}

	internalResponse, err := (&Outbound{}).TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if internalResponse.Model != "jev-1.13.0" {
		t.Fatalf("expected versioned upstream model, got %q", internalResponse.Model)
	}
	if !bytes.Equal(internalResponse.SystemOnePayload, raw) {
		t.Fatalf("expected provider response to be preserved, got %s", internalResponse.SystemOnePayload)
	}
	if !internalResponse.IsSystemOneResponse() {
		t.Fatal("expected system one response")
	}
	if internalResponse.Usage == nil {
		t.Fatal("expected token usage")
	}
	if internalResponse.Usage.PromptTokens != 392 || internalResponse.Usage.CompletionTokens != 65 || internalResponse.Usage.TotalTokens != 457 {
		t.Fatalf("unexpected token usage: %+v", internalResponse.Usage)
	}
}

func TestTransformResponseAllowsMissingUsage(t *testing.T) {
	raw := `{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.5}}}`
	response := &http.Response{Body: io.NopCloser(strings.NewReader(raw))}

	internalResponse, err := (&Outbound{}).TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatalf("TransformResponse returned error: %v", err)
	}
	if internalResponse.Usage != nil {
		t.Fatalf("expected nil usage, got %+v", internalResponse.Usage)
	}
}

func TestTransformResponseRejectsInvalidAnswers(t *testing.T) {
	tests := []string{
		``,
		`   `,
		`{`,
		`{}`,
		`{"model":"jev-1.13.0","answers":null}`,
		`{"model":"jev-1.13.0","answers":{}}`,
		`{"model":"jev-1.13.0","answers":[]}`,
		`{"model":"jev-1.13.0","answers":"not-an-object"}`,
	}

	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			response := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
			if _, err := (&Outbound{}).TransformResponse(context.Background(), response); err == nil {
				t.Fatalf("expected invalid answers payload %q to fail", body)
			}
		})
	}
}

func TestTransformStreamIsUnsupported(t *testing.T) {
	if _, err := (&Outbound{}).TransformStream(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected streaming to be unsupported")
	}
}

func jsonEqual(t *testing.T, left, right json.RawMessage) bool {
	t.Helper()
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		t.Fatalf("decode %s: %v", left, err)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		t.Fatalf("decode %s: %v", right, err)
	}
	leftJSON, _ := json.Marshal(leftValue)
	rightJSON, _ := json.Marshal(rightValue)
	return bytes.Equal(leftJSON, rightJSON)
}
