package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// buildChatRequestWithToolCall 造一个客户端回传历史 tool_calls 的 Chat 请求体。
// 客户端不可能带上 thoughtSignature——Chat 协议没这个字段，网关必须自己补。
func buildChatRequestWithToolCall(t *testing.T, callID, callName string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model": "gemini-3.6-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "查一下"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":       callID,
						"type":     "function",
						"index":    0,
						"function": map[string]any{"name": callName, "arguments": "{}"},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": callID, "content": "结果"},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return body
}

func firstToolCallSignature(t *testing.T, req *model.InternalLLMRequest) string {
	t.Helper()
	for idx := range req.Messages {
		if len(req.Messages[idx].ToolCalls) > 0 {
			return req.Messages[idx].ToolCalls[0].ThoughtSignature
		}
	}
	return ""
}

// 非流式：响应里的签名存下来，下一轮请求按 tool call ID 回填。
func TestChatRoundTripRestoresGeminiThoughtSignature(t *testing.T) {
	const callID = "call_chat_nonstream_search_0"
	inbound := &ChatInbound{}

	if _, err := inbound.TransformResponse(context.Background(), &model.InternalLLMResponse{
		Choices: []model.Choice{{
			Index: 0,
			Message: &model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID:               callID,
					Type:             "function",
					Function:         model.FunctionCall{Name: "session_search"},
					ThoughtSignature: "sig-nonstream",
				}},
			},
		}},
	}); err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}

	req, err := inbound.TransformRequest(context.Background(), buildChatRequestWithToolCall(t, callID, "session_search"))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if got := firstToolCallSignature(t, req); got != "sig-nonstream" {
		t.Fatalf("thoughtSignature not restored, got %q want %q", got, "sig-nonstream")
	}
}

// 流式：签名挂在 tool call 事件上，同样要能存下来并回填。
func TestChatStreamEventsRestoreGeminiThoughtSignature(t *testing.T) {
	const callID = "call_chat_stream_search_0"
	inbound := &ChatInbound{}

	toolCall := model.ToolCall{
		ID:       callID,
		Type:     "function",
		Index:    0,
		Function: model.FunctionCall{Name: "session_search"},
		ProviderExtensions: &model.ProviderExtensions{
			Gemini: &model.GeminiExtension{ThoughtSignature: "sig-stream"},
		},
	}
	if _, err := inbound.TransformStreamEvents(context.Background(), []model.StreamEvent{
		{Kind: model.StreamEventKindToolCallStart, Index: 0, ToolCall: &toolCall},
	}); err != nil {
		t.Fatalf("TransformStreamEvents: %v", err)
	}

	req, err := inbound.TransformRequest(context.Background(), buildChatRequestWithToolCall(t, callID, "session_search"))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if got := firstToolCallSignature(t, req); got != "sig-stream" {
		t.Fatalf("thoughtSignature not restored, got %q want %q", got, "sig-stream")
	}
}

// 并行工具调用：每个 functionCall 的签名必须各归各的，串了 Gemini 一样会 400。
func TestChatParallelToolCallsKeepDistinctSignatures(t *testing.T) {
	inbound := &ChatInbound{}
	if _, err := inbound.TransformResponse(context.Background(), &model.InternalLLMResponse{
		Choices: []model.Choice{{
			Index: 0,
			Message: &model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{
					{ID: "call_parallel_a_0", Type: "function", Index: 0, Function: model.FunctionCall{Name: "search_a"}, ThoughtSignature: "sig-a"},
					{ID: "call_parallel_b_1", Type: "function", Index: 1, Function: model.FunctionCall{Name: "search_b"}, ThoughtSignature: "sig-b"},
				},
			},
		}},
	}); err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}

	body, err := json.Marshal(map[string]any{
		"model": "gemini-3.6-flash",
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{"id": "call_parallel_a_0", "type": "function", "index": 0, "function": map[string]any{"name": "search_a", "arguments": "{}"}},
					map[string]any{"id": "call_parallel_b_1", "type": "function", "index": 1, "function": map[string]any{"name": "search_b", "arguments": "{}"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	toolCalls := req.Messages[0].ToolCalls
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	if toolCalls[0].ThoughtSignature != "sig-a" || toolCalls[1].ThoughtSignature != "sig-b" {
		t.Fatalf("signatures swapped or missing: %q / %q", toolCalls[0].ThoughtSignature, toolCalls[1].ThoughtSignature)
	}
}

// 没有签名可回填时不应凭空造值，避免把无关的 tool call 污染成带签名。
func TestChatRequestWithoutCachedSignatureStaysEmpty(t *testing.T) {
	inbound := &ChatInbound{}
	req, err := inbound.TransformRequest(context.Background(), buildChatRequestWithToolCall(t, "call_never_seen_9", "unknown_tool"))
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if got := firstToolCallSignature(t, req); got != "" {
		t.Fatalf("unexpected signature %q", got)
	}
}
