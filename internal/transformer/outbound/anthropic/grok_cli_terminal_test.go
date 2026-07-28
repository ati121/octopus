package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
)

func TestGrokCLIImageToolResultReachesAnthropic(t *testing.T) {
	ctx := context.Background()
	internalReq, err := (&openaiInbound.ResponseInbound{}).TransformRequest(ctx, []byte(`{
		"model":"claude-opus-4-7",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"inspect the image"}]},
			{"type":"function_call","id":"fc_read","call_id":"call_read","name":"read_file","arguments":"{\"target_file\":\"image.png\"}"},
			{"type":"function_call_output","call_id":"call_read","output":[
				{"type":"input_text","text":"Read image file: image.png"},
				{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="}
			]}
		]
	}`))
	if err != nil {
		t.Fatalf("transform Responses request: %v", err)
	}

	httpReq, err := (&MessageOutbound{}).TransformRequest(ctx, internalReq, "https://api.anthropic.com", "sk-test")
	if err != nil {
		t.Fatalf("transform Anthropic request: %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read Anthropic request body: %v", err)
	}

	var payload anthropicModel.MessageRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal Anthropic request: %v", err)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("expected user, assistant tool call, and tool result messages, got %d: %s", len(payload.Messages), body)
	}
	toolResults := payload.Messages[2].Content.MultipleContent
	if len(toolResults) != 1 || toolResults[0].Type != "tool_result" || toolResults[0].Content == nil {
		t.Fatalf("expected one tool_result block, got %+v", toolResults)
	}
	resultContent := toolResults[0].Content.MultipleContent
	if len(resultContent) != 2 {
		t.Fatalf("expected text and image in tool_result content, got %+v", resultContent)
	}
	if resultContent[0].Type != "text" || resultContent[0].Text == nil || *resultContent[0].Text != "Read image file: image.png" {
		t.Fatalf("tool result text was not preserved: %+v", resultContent[0])
	}
	image := resultContent[1]
	if image.Type != "image" || image.Source == nil || image.Source.Type != "base64" || image.Source.MediaType != "image/png" || image.Source.Data != "aW1hZ2U=" {
		t.Fatalf("tool result image was not converted for Anthropic: %+v", image)
	}
}

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
