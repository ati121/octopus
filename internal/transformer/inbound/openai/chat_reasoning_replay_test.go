package openai

import (
	"context"
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// 流式：thinking 与 tool_calls 的 delta 跨多个分片，聚合完成后按 tool call ID
// 存档，下一轮客户端重放（历史里的 assistant 轮次带空格占位）要能补回真实正文。
// 现场来源：Hermes 对没有思维过程的轮次填「一个空格」，DeepSeek V4 thinking
// 上游以 "The `reasoning_content` in the thinking mode must be passed back to
// the API." 400 拒绝整轮请求。
func TestChatInboundRestoresStreamedReasoningContent(t *testing.T) {
	inbound := &ChatInbound{}
	ctx := context.Background()

	chunks := []*model.InternalLLMResponse{
		{ID: "chat_stream_1", Object: "chat.completion.chunk", Choices: []model.Choice{{
			Index: 0, Delta: &model.Message{Role: "assistant", ReasoningContent: lo.ToPtr("先读配置，")},
		}}},
		{ID: "chat_stream_1", Object: "chat.completion.chunk", Choices: []model.Choice{{
			Index: 0, Delta: &model.Message{ReasoningContent: lo.ToPtr("再改插件设置。")},
		}}},
		{ID: "chat_stream_1", Object: "chat.completion.chunk", Choices: []model.Choice{{
			Index: 0, Delta: &model.Message{ToolCalls: []model.ToolCall{{
				Index: 0, ID: "call_chat_stream_rc_0", Type: "function",
				Function: model.FunctionCall{Name: "read", Arguments: `{"path":"config.yaml"}`},
			}}},
		}}},
	}
	for _, chunk := range chunks {
		if _, err := inbound.TransformStream(ctx, chunk); err != nil {
			t.Fatalf("TransformStream failed: %v", err)
		}
	}

	// 聚合后存档（GetInternalResponse 是流式结束时 relay 取完整响应的入口）
	if _, err := inbound.GetInternalResponse(ctx); err != nil {
		t.Fatalf("GetInternalResponse failed: %v", err)
	}

	messages := chatReplayToolCallTurn(t, inbound, "call_chat_stream_rc_0")
	if got := messages[1].GetReasoningContent(); got != "先读配置，再改插件设置。" {
		t.Fatalf("reasoning_content not restored: %q (%#v)", got, messages[1])
	}
}

// 非流式响应走 TransformResponse，同样要存档。
func TestChatInboundRestoresNonStreamReasoningContent(t *testing.T) {
	inbound := &ChatInbound{}
	response := &model.InternalLLMResponse{
		ID:     "chat_nonstream_1",
		Model:  "deepseek-v4-flash",
		Object: "chat.completion",
		Choices: []model.Choice{{
			Index:        0,
			FinishReason: lo.ToPtr("tool_calls"),
			Message: &model.Message{
				Role:             "assistant",
				ReasoningContent: lo.ToPtr("需要先确认配置内容。"),
				ToolCalls: []model.ToolCall{{
					ID: "call_chat_nonstream_rc_0", Type: "function",
					Function: model.FunctionCall{Name: "read", Arguments: `{"path":"config.yaml"}`},
				}},
			},
		}},
	}
	if _, err := inbound.TransformResponse(context.Background(), response); err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	messages := chatReplayToolCallTurn(t, inbound, "call_chat_nonstream_rc_0")
	if got := messages[1].GetReasoningContent(); got != "需要先确认配置内容。" {
		t.Fatalf("reasoning_content not restored: %q (%#v)", got, messages[1])
	}
}

// 客户端自己带回了真实推理正文（非空白）时不能被缓存里的旧值覆盖。
func TestChatRestoreKeepsClientProvidedReasoning(t *testing.T) {
	inbound := &ChatInbound{}
	if _, err := inbound.TransformResponse(context.Background(), &model.InternalLLMResponse{
		Choices: []model.Choice{{Message: &model.Message{
			Role:             "assistant",
			ReasoningContent: lo.ToPtr("缓存里的旧正文"),
			ToolCalls: []model.ToolCall{{
				ID: "call_chat_keep_rc_0", Function: model.FunctionCall{Name: "read"},
			}},
		}}},
	}); err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	messages := chatTransform(t, inbound, `[
		{"role":"user","content":"读一下配置"},
		{"role":"assistant","content":"读完了","reasoning_content":"客户端带回的正文","tool_calls":[{"id":"call_chat_keep_rc_0","type":"function","function":{"name":"read","arguments":"{}"}}]}
	]`)
	if got := messages[1].GetReasoningContent(); got != "客户端带回的正文" {
		t.Fatalf("client reasoning overwritten: %q", got)
	}
}

// 缓存里没有记录时保持空白占位原样，不能凭空造正文。
func TestChatInboundLeavesUnknownReasoningTurnsAlone(t *testing.T) {
	inbound := &ChatInbound{}
	messages := chatTransform(t, inbound, `[
		{"role":"user","content":"读一下配置"},
		{"role":"assistant","content":"","reasoning_content":" ","tool_calls":[{"id":"call_chat_never_seen_0","type":"function","function":{"name":"read","arguments":"{}"}}]}
	]`)

	got := messages[1].GetReasoningContent()
	if got != " " && got != "" {
		t.Fatalf("unexpected reasoning_content %q", got)
	}
}

// chatTransform 用 ChatInbound 转换一个完整的 Chat 请求体，返回内部消息。
func chatTransform(t *testing.T, inbound *ChatInbound, body string) []model.Message {
	t.Helper()
	req, err := inbound.TransformRequest(context.Background(), []byte(`{"model":"deepseek-v4-flash","messages":`+body+`}`))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	return req.Messages
}

// chatReplayToolCallTurn 模拟客户端把某个 tool call 通过 Chat 协议重放给网关。
// 返回的第 0 条是 user，第 1 条是带 tool_calls、推理正文为空格占位的 assistant。
func chatReplayToolCallTurn(t *testing.T, inbound *ChatInbound, callID string) []model.Message {
	t.Helper()
	// Hermes 风格的历史：assistant 轮次带 tool_calls，reasoning_content 是「一个空格」占位
	return chatTransform(t, inbound, `[
		{"role":"user","content":"帮我看看配置"},
		{"role":"assistant","content":"","reasoning_content":" ","tool_calls":[{"id":"`+callID+`","type":"function","function":{"name":"read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"`+callID+`","content":"ok"}
	]`)
}
