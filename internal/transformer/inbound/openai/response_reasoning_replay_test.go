package openai

import (
	"context"
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// 流式：推理正文经 Responses 出站时只剩摘要，下一轮重放要能按 tool call ID 补回来。
func TestResponseInboundRestoresStreamedReasoningContent(t *testing.T) {
	out := &ResponseInbound{}
	events := []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "resp_stream_1", Model: "deepseek-v4-flash"},
		{Kind: model.StreamEventKindThinkingDelta, Delta: &model.StreamDelta{Thinking: "先读 README，"}},
		{Kind: model.StreamEventKindThinkingDelta, Delta: &model.StreamDelta{Thinking: "再决定下一步。"}},
		{Kind: model.StreamEventKindToolCallStart, ToolCall: &model.ToolCall{
			Index: 0, ID: "call_stream_rc_0", Type: "function",
			Function: model.FunctionCall{Name: "read", Arguments: `{"path":"README.md"}`},
		}},
		{Kind: model.StreamEventKindMessageStop, StopReason: model.FinishReasonToolCalls},
	}
	if _, err := out.TransformStreamEvents(context.Background(), events); err != nil {
		t.Fatalf("TransformStreamEvents failed: %v", err)
	}

	messages := replayToolCallTurn(t, "call_stream_rc_0", "read")
	if got := messages[1].GetReasoningContent(); got != "先读 README，再决定下一步。" {
		t.Fatalf("reasoning_content not restored: %q (%#v)", got, messages[1])
	}
}

// 非流式走 TransformResponse，同样要存档。
func TestResponseInboundRestoresNonStreamReasoningContent(t *testing.T) {
	out := &ResponseInbound{}
	response := &model.InternalLLMResponse{
		ID:     "resp_nonstream_1",
		Model:  "deepseek-v4-flash",
		Object: "chat.completion",
		Choices: []model.Choice{{
			Index:        0,
			FinishReason: lo.ToPtr("tool_calls"),
			Message: &model.Message{
				Role:             "assistant",
				ReasoningContent: lo.ToPtr("需要先确认文件内容。"),
				ToolCalls: []model.ToolCall{{
					ID: "call_nonstream_rc_0", Type: "function",
					Function: model.FunctionCall{Name: "read", Arguments: `{"path":"README.md"}`},
				}},
			},
		}},
	}
	if _, err := out.TransformResponse(context.Background(), response); err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	messages := replayToolCallTurn(t, "call_nonstream_rc_0", "read")
	if got := messages[1].GetReasoningContent(); got != "需要先确认文件内容。" {
		t.Fatalf("reasoning_content not restored: %q (%#v)", got, messages[1])
	}
}

// 客户端自己带回了推理正文时不能被缓存里的旧值覆盖。
func TestRestoreReasoningContentKeepsClientProvided(t *testing.T) {
	out := &ResponseInbound{}
	if _, err := out.TransformResponse(context.Background(), &model.InternalLLMResponse{
		Choices: []model.Choice{{Message: &model.Message{
			Role:             "assistant",
			ReasoningContent: lo.ToPtr("缓存里的旧正文"),
			ToolCalls: []model.ToolCall{{
				ID: "call_keep_rc_0", Function: model.FunctionCall{Name: "read"},
			}},
		}}},
	}); err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	messages := []model.Message{{
		Role:             "assistant",
		ReasoningContent: lo.ToPtr("客户端带回的正文"),
		ToolCalls: []model.ToolCall{{
			ID: "call_keep_rc_0", Function: model.FunctionCall{Name: "read"},
		}},
	}}
	restoreReasoningContent(messages)

	if got := messages[0].GetReasoningContent(); got != "客户端带回的正文" {
		t.Fatalf("client reasoning overwritten: %q", got)
	}
}

// 缓存里没有记录时保持为空，不能凭空造正文喂给上游。
func TestRestoreReasoningContentLeavesUnknownTurnsAlone(t *testing.T) {
	messages := replayToolCallTurn(t, "call_never_seen_0", "read")
	if got := messages[1].GetReasoningContent(); got != "" {
		t.Fatalf("unexpected reasoning_content %q", got)
	}
}

// replayToolCallTurn 模拟客户端把某个 tool call 回放给网关，返回转换后的 Chat 消息。
// 返回的第 0 条是 user，第 1 条是带 tool_calls 的 assistant。
func replayToolCallTurn(t *testing.T, callID, name string) []model.Message {
	t.Helper()

	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("读一下 README")}},
		{Type: "reasoning", Summary: []ResponsesReasoningSummary{}, EncryptedContent: lo.ToPtr("sig-1")},
		{ID: "fc_1", Type: "function_call", CallID: callID, Name: name, Arguments: `{"path":"README.md"}`},
		{Type: "function_call_output", CallID: callID, Output: &ResponsesInput{Text: stringPtr("# Octopus")}},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) < 2 || messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("unexpected replay shape: %#v", messages)
	}

	return messages
}
