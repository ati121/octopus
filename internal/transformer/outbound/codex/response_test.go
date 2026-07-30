package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestResponseOutboundTransformRequest(t *testing.T) {
	content := "hello"
	outbound := &ResponseOutbound{}
	request := &model.InternalLLMRequest{
		Model: "codex-mini-latest",
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: &content,
			},
		}},
	}

	req, err := outbound.TransformRequest(context.Background(), request, "https://codex.example/v1", "test-key")
	if err != nil {
		t.Fatalf("TransformRequest returned error: %v", err)
	}
	if got, want := req.URL.String(), "https://codex.example/v1/responses"; got != want {
		t.Fatalf("request URL: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get("Authorization"), "Bearer test-key"; got != want {
		t.Fatalf("Authorization: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get(headerOriginator), defaultOriginator; got != want {
		t.Fatalf("originator: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get(headerOpenAIBeta), defaultOpenAIBetaVal; got != want {
		t.Fatalf("OpenAI-Beta: got %q, want %q", got, want)
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if got := string(payload["model"]); got != `"codex-mini-latest"` {
		t.Fatalf("model: got %s", got)
	}
	if _, ok := payload["input"]; !ok {
		t.Fatalf("responses payload has no input: %s", payload)
	}
}

func TestResponseOutboundTransformRequestRaw(t *testing.T) {
	outbound := &ResponseOutbound{}
	rawBody := []byte(`{"model":"client-model","input":"hello","future_parameter":{"keep":true}}`)
	query := url.Values{"trace": {"enabled"}}

	req, err := outbound.TransformRequestRaw(context.Background(), rawBody, "codex-mini-latest", "https://codex.example/v1", "test-key", query)
	if err != nil {
		t.Fatalf("TransformRequestRaw returned error: %v", err)
	}
	if got, want := req.URL.String(), "https://codex.example/v1/responses?trace=enabled"; got != want {
		t.Fatalf("request URL: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get(headerOriginator), defaultOriginator; got != want {
		t.Fatalf("originator: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get(headerOpenAIBeta), defaultOpenAIBetaVal; got != want {
		t.Fatalf("OpenAI-Beta: got %q, want %q", got, want)
	}

	var payload map[string]json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if got := string(payload["model"]); got != `"codex-mini-latest"` {
		t.Fatalf("model rewrite: got %s", got)
	}
	if got := string(payload["future_parameter"]); got != `{"keep":true}` {
		t.Fatalf("future field not preserved: got %s", got)
	}
}

func TestApplyCodexHeadersDoesNotOverrideExistingValues(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://codex.example/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(headerOriginator, "custom-originator")
	req.Header.Set(headerOpenAIBeta, "custom-beta")

	applyCodexHeaders(req)

	if got, want := req.Header.Get(headerOriginator), "custom-originator"; got != want {
		t.Fatalf("originator overwritten: got %q, want %q", got, want)
	}
	if got, want := req.Header.Get(headerOpenAIBeta), "custom-beta"; got != want {
		t.Fatalf("OpenAI-Beta overwritten: got %q, want %q", got, want)
	}
}

func TestResponseOutboundPassthroughCapability(t *testing.T) {
	outbound := &ResponseOutbound{}
	if !outbound.CanPassthrough(model.APIFormatOpenAIResponse) {
		t.Fatal("Codex should support OpenAI Responses raw passthrough")
	}
	if outbound.CanPassthrough(model.APIFormatOpenAIChatCompletion) {
		t.Fatal("Codex must not claim OpenAI Chat raw passthrough")
	}
	cfg := outbound.PassthroughConfig()
	if cfg.CollectMetrics {
		t.Fatal("Codex should inherit Responses passthrough metrics semantics (CollectMetrics=false)")
	}
	if _, ok := cfg.TerminalEvents["response.completed"]; !ok {
		t.Fatal("Codex should inherit Responses terminal events")
	}
}
