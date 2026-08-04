package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// findEmptyAssistant 返回第一条既无 content 又无 tool_calls 的 assistant 消息下标，没有则返回 -1。
func findEmptyAssistant(messages []model.Message) int {
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		if len(messages[i].ToolCalls) > 0 {
			continue
		}
		if messages[i].Content.Content != nil && *messages[i].Content.Content != "" {
			continue
		}
		if len(messages[i].Content.MultipleContent) > 0 {
			continue
		}
		return i
	}

	return -1
}

func TestConvertInputToMessagesMergesAssistantTextIntoToolCallMessage(t *testing.T) {
	output := ResponsesInput{Text: stringPtr("done")}
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("查一下")}},
		{Type: "message", Role: "assistant", Content: &ResponsesInput{Text: stringPtr("我看看")}},
		{ID: "fc_1", Type: "function_call", CallID: "call_1", Name: "lookup", Arguments: `{}`},
		{Type: "function_call_output", CallID: "call_1", Output: &output},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user + assistant(text+tool_calls) + tool, got %#v", messages)
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("function_call was not merged into the preceding assistant message: %#v", messages)
	}
	if messages[1].Content.Content == nil || *messages[1].Content.Content != "我看看" {
		t.Fatalf("assistant text was lost while merging tool calls: %#v", messages[1].Content)
	}
	if messages[2].Role != "tool" {
		t.Fatalf("tool output order changed: %#v", messages)
	}
}

func TestConvertInputToMessagesDropsEmptyAssistantBeforeToolCall(t *testing.T) {
	// opencode 的真实形状：assistant 条目 content 为空串，紧跟着独立的 function_call 条目。
	output := ResponsesInput{Text: stringPtr("done")}
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("查一下")}},
		{Type: "message", Role: "assistant", Content: &ResponsesInput{Text: stringPtr("")}},
		{ID: "fc_1", Type: "function_call", CallID: "call_1", Name: "lookup", Arguments: `{}`},
		{Type: "function_call_output", CallID: "call_1", Output: &output},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if idx := findEmptyAssistant(messages); idx >= 0 {
		t.Fatalf("message %d has neither content nor tool_calls: %#v", idx, messages)
	}
	if len(messages) != 3 || messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected the empty assistant item to carry the tool call, got %#v", messages)
	}
	if messages[1].ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool call id was not preserved: %#v", messages[1].ToolCalls)
	}
}

func TestConvertInputToMessagesMovesReasoningOntoFollowingAssistant(t *testing.T) {
	sig := "enc-sig-1"
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("查一下")}},
		{Type: "reasoning", Summary: []ResponsesReasoningSummary{{Type: "summary_text", Text: "先搜索"}}, EncryptedContent: &sig},
		{Type: "message", Role: "assistant", Content: &ResponsesInput{Text: stringPtr("")}},
		{ID: "fc_1", Type: "function_call", CallID: "call_1", Name: "lookup", Arguments: `{}`},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if idx := findEmptyAssistant(messages); idx >= 0 {
		t.Fatalf("message %d has neither content nor tool_calls: %#v", idx, messages)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected user + assistant(tool_calls), got %#v", messages)
	}
	if messages[1].ReasoningContent == nil || *messages[1].ReasoningContent != "先搜索" {
		t.Fatalf("reasoning text was not carried onto the tool call message: %#v", messages[1].ReasoningContent)
	}
	if messages[1].ReasoningSignature == nil || *messages[1].ReasoningSignature != sig {
		t.Fatalf("reasoning signature was not carried onto the tool call message: %#v", messages[1].ReasoningSignature)
	}
}

func TestConvertInputToMessagesChainsConsecutiveReasoningItems(t *testing.T) {
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "reasoning", Summary: []ResponsesReasoningSummary{{Type: "summary_text", Text: "A"}}},
		{Type: "reasoning", Summary: []ResponsesReasoningSummary{{Type: "summary_text", Text: "B"}}},
		{Type: "message", Role: "assistant", Content: &ResponsesInput{Text: stringPtr("答案")}},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "assistant" {
		t.Fatalf("expected the reasoning chain to collapse into one assistant message, got %#v", messages)
	}
	if messages[0].ReasoningContent == nil || *messages[0].ReasoningContent != "AB" {
		t.Fatalf("reasoning chain order or content changed: %#v", messages[0].ReasoningContent)
	}
}

func TestConvertInputToMessagesDropsReasoningBeforeUserTurn(t *testing.T) {
	// 推理块和后一条 user 消息不属于同一轮，宁可丢弃也不能挂到下一轮头上。
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "reasoning", Summary: []ResponsesReasoningSummary{{Type: "summary_text", Text: "上一轮的想法"}}},
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("再来一次")}},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("expected only the user message to survive, got %#v", messages)
	}
	if messages[0].ReasoningContent != nil {
		t.Fatalf("reasoning must not leak across a user turn: %#v", messages[0].ReasoningContent)
	}
}

func TestConvertInputToMessagesKeepsOrphanFunctionCallMessage(t *testing.T) {
	// function_call 前面不是 assistant 时仍要单独成条，不能挂到 user 上。
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("查一下")}},
		{ID: "fc_1", Type: "function_call", CallID: "call_1", Name: "lookup", Arguments: `{}`},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || len(messages[0].ToolCalls) != 0 {
		t.Fatalf("tool call must not be attached to the user message: %#v", messages)
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected a standalone assistant tool call message, got %#v", messages)
	}
}

func TestResponseInboundEmitsNoEmptyAssistantMessage(t *testing.T) {
	// opencode 触发 400 的完整序列：reasoning(空 summary) → assistant("") → function_call
	// → function_call_output，重复两轮。
	body := []byte(`{
		"model":"deepseek-v4-flash",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"读一下 README"}]},
			{"type":"reasoning","summary":[],"encrypted_content":"sig-1"},
			{"type":"message","role":"assistant","content":""},
			{"id":"fc_1","type":"function_call","call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"# Octopus"},
			{"type":"reasoning","summary":[],"encrypted_content":"sig-2"},
			{"type":"message","role":"assistant","content":""},
			{"id":"fc_2","type":"function_call","call_id":"call_2","name":"read","arguments":"{\"path\":\"go.mod\"}"},
			{"type":"function_call_output","call_id":"call_2","output":"module octopus"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"看完了"}]}
		]
	}`)

	req, err := (&ResponseInbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	if idx := findEmptyAssistant(req.Messages); idx >= 0 {
		t.Fatalf("message %d has neither content nor tool_calls: %#v", idx, req.Messages)
	}

	// 序列化后再确认一次：上游看到的是 JSON，不是内部结构体。
	raw, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatalf("marshal messages failed: %v", err)
	}
	var serialized []map[string]any
	if err := json.Unmarshal(raw, &serialized); err != nil {
		t.Fatalf("unmarshal messages failed: %v", err)
	}
	for i, msg := range serialized {
		if msg["role"] != "assistant" {
			continue
		}
		if _, hasToolCalls := msg["tool_calls"]; hasToolCalls {
			continue
		}
		if content, ok := msg["content"].(string); ok && content != "" {
			continue
		}
		t.Fatalf("serialized assistant message %d has no content and no tool_calls: %s", i, raw)
	}

	if len(req.Messages) != 6 {
		t.Fatalf("expected user + 2×(assistant tool call + tool) + final assistant, got %#v", req.Messages)
	}
	if len(req.Messages[1].ToolCalls) != 1 || req.Messages[1].ReasoningSignature == nil || *req.Messages[1].ReasoningSignature != "sig-1" {
		t.Fatalf("first round lost its tool call or signature: %#v", req.Messages[1])
	}
	if len(req.Messages[3].ToolCalls) != 1 || req.Messages[3].ReasoningSignature == nil || *req.Messages[3].ReasoningSignature != "sig-2" {
		t.Fatalf("second round lost its tool call or signature: %#v", req.Messages[3])
	}
}
