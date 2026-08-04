package compat

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestSaveAndRestoreReasoningContent(t *testing.T) {
	toolCalls := []model.ToolCall{
		{ID: "call_reason_a_0", Function: model.FunctionCall{Name: "read_file"}},
		{ID: "call_reason_a_1", Function: model.FunctionCall{Name: "grep"}},
	}
	SaveReasoningContent(toolCalls, "先看文件，再搜索关键词。")

	if got := RestoreReasoningContent(toolCalls); got != "先看文件，再搜索关键词。" {
		t.Fatalf("got %q", got)
	}
}

// 一轮推理对应一整批 tool call，任意一个 ID 都该能取回同一份正文 ——
// 重放时客户端可能只带回其中一部分。
func TestRestoreReasoningContentFromAnySingleToolCall(t *testing.T) {
	SaveReasoningContent([]model.ToolCall{
		{ID: "call_reason_b_0", Function: model.FunctionCall{Name: "read_file"}},
		{ID: "call_reason_b_1", Function: model.FunctionCall{Name: "grep"}},
	}, "同一轮的推理")

	only := []model.ToolCall{{ID: "call_reason_b_1", Function: model.FunctionCall{Name: "grep"}}}
	if got := RestoreReasoningContent(only); got != "同一轮的推理" {
		t.Fatalf("got %q", got)
	}
}

// 客户端改了工具名（或压根拿不到）时要走不带 tool name 的兜底键。
func TestRestoreReasoningContentIgnoresToolNameMismatch(t *testing.T) {
	SaveReasoningContent([]model.ToolCall{
		{ID: "call_reason_c_0", Function: model.FunctionCall{Name: "read_file"}},
	}, "推理正文 C")

	replayed := []model.ToolCall{{ID: "call_reason_c_0", Function: model.FunctionCall{Name: "renamed_tool"}}}
	if got := RestoreReasoningContent(replayed); got != "推理正文 C" {
		t.Fatalf("got %q", got)
	}
}

// 缓存里没有记录时返回空，不能凭空编造推理内容回传给上游。
func TestRestoreReasoningContentMissesQuietly(t *testing.T) {
	replayed := []model.ToolCall{{ID: "call_reason_unknown_9", Function: model.FunctionCall{Name: "nope"}}}
	if got := RestoreReasoningContent(replayed); got != "" {
		t.Fatalf("unexpected reasoning %q", got)
	}
}

// 空正文不写缓存，避免用空串占掉 key。
func TestSaveReasoningContentIgnoresEmpty(t *testing.T) {
	toolCalls := []model.ToolCall{{ID: "call_reason_empty_0", Function: model.FunctionCall{Name: "read_file"}}}
	SaveReasoningContent(toolCalls, "")
	SaveReasoningContent(toolCalls, "   ")

	if got := RestoreReasoningContent(toolCalls); got != "" {
		t.Fatalf("unexpected reasoning %q", got)
	}
}

// 两轮不同的推理各存各的，不能串台。
func TestSaveReasoningContentKeepsTurnsSeparate(t *testing.T) {
	turn1 := []model.ToolCall{{ID: "call_reason_t1_0", Function: model.FunctionCall{Name: "read_file"}}}
	turn2 := []model.ToolCall{{ID: "call_reason_t2_0", Function: model.FunctionCall{Name: "read_file"}}}
	SaveReasoningContent(turn1, "第一轮的推理")
	SaveReasoningContent(turn2, "第二轮的推理")

	if got := RestoreReasoningContent(turn1); got != "第一轮的推理" {
		t.Fatalf("turn1: got %q", got)
	}
	if got := RestoreReasoningContent(turn2); got != "第二轮的推理" {
		t.Fatalf("turn2: got %q", got)
	}
}
