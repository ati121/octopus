package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
)

func TestAnthropicEmptyMessageStopCompletesResponsesStream(t *testing.T) {
	outbound := &MessageOutbound{}
	inbound := &openaiInbound.ResponseInbound{}
	ctx := context.Background()

	var wire bytes.Buffer
	for _, payload := range [][]byte{
		[]byte(`{"type":"message_start","message":{"id":"resp_grok_cli","model":"claude-test","role":"assistant","usage":{"input_tokens":12,"output_tokens":0}}}`),
		[]byte(`{"type":"message_stop"}`),
	} {
		events, err := outbound.TransformStreamEvent(ctx, payload)
		if err != nil {
			t.Fatalf("TransformStreamEvent(%s): %v", payload, err)
		}
		chunk, err := inbound.TransformStreamEvents(ctx, events)
		if err != nil {
			t.Fatalf("TransformStreamEvents: %v", err)
		}
		wire.Write(chunk)
	}

	type rawResponse struct {
		Output json.RawMessage `json:"output"`
	}
	type rawEvent struct {
		Type     string       `json:"type"`
		Response *rawResponse `json:"response"`
	}

	foundCompleted := false
	for _, block := range bytes.Split(wire.Bytes(), []byte("\n\n")) {
		payload := bytes.TrimPrefix(bytes.TrimSpace(block), []byte("data: "))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}

		var event rawEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("invalid Responses SSE event %q: %v", payload, err)
		}
		if event.Response != nil {
			output := bytes.TrimSpace(event.Response.Output)
			if len(output) == 0 || bytes.Equal(output, []byte("null")) {
				t.Fatalf("%s encoded response.output as %s", event.Type, output)
			}
		}
		if event.Type != "response.completed" {
			continue
		}
		foundCompleted = true

		var output []struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(event.Response.Output, &output); err != nil {
			t.Fatalf("response.completed.output is not an array: %v", err)
		}
		if len(output) != 1 || output[0].Type != "message" {
			t.Fatalf("unexpected synthesized output: %s", event.Response.Output)
		}

		var content []struct {
			Type        string          `json:"type"`
			Annotations json.RawMessage `json:"annotations"`
		}
		if err := json.Unmarshal(output[0].Content, &content); err != nil {
			t.Fatalf("message content is not an array: %v", err)
		}
		if len(content) != 1 || content[0].Type != "output_text" || !bytes.Equal(bytes.TrimSpace(content[0].Annotations), []byte("[]")) {
			t.Fatalf("Grok CLI-safe output_text shape not preserved: %s", output[0].Content)
		}
	}

	if !foundCompleted {
		t.Fatalf("response.completed not emitted; wire=%s", wire.String())
	}
}
