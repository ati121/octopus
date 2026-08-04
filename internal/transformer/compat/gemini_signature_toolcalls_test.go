package compat

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestSaveAndRestoreGeminiToolCallSignatures(t *testing.T) {
	saved := []model.ToolCall{
		{ID: "call_compat_a_0", Function: model.FunctionCall{Name: "search_a"}, ThoughtSignature: "sig-a"},
		{ID: "call_compat_b_1", Function: model.FunctionCall{Name: "search_b"},
			ProviderExtensions: &model.ProviderExtensions{Gemini: &model.GeminiExtension{ThoughtSignature: "sig-b"}}},
	}
	SaveGeminiToolCallSignatures(saved)

	replayed := []model.ToolCall{
		{ID: "call_compat_a_0", Function: model.FunctionCall{Name: "search_a"}},
		{ID: "call_compat_b_1", Function: model.FunctionCall{Name: "search_b"}},
	}
	RestoreGeminiToolCallSignatures(replayed)

	if replayed[0].ThoughtSignature != "sig-a" {
		t.Fatalf("tool call a: got %q want %q", replayed[0].ThoughtSignature, "sig-a")
	}
	// 签名写在 ProviderExtensions 上时同样要能存取。
	if replayed[1].ThoughtSignature != "sig-b" {
		t.Fatalf("tool call b: got %q want %q", replayed[1].ThoughtSignature, "sig-b")
	}
}

// 已经带签名的条目不能被缓存里的旧值覆盖。
func TestRestoreGeminiToolCallSignaturesKeepsExisting(t *testing.T) {
	SaveGeminiToolCallSignatures([]model.ToolCall{
		{ID: "call_compat_keep_0", Function: model.FunctionCall{Name: "search"}, ThoughtSignature: "sig-cached"},
	})

	replayed := []model.ToolCall{
		{ID: "call_compat_keep_0", Function: model.FunctionCall{Name: "search"}, ThoughtSignature: "sig-incoming"},
	}
	RestoreGeminiToolCallSignatures(replayed)

	if replayed[0].ThoughtSignature != "sig-incoming" {
		t.Fatalf("existing signature overwritten: got %q", replayed[0].ThoughtSignature)
	}
}

// 缓存里没有对应记录时保持为空，不能凭空造签名。
func TestRestoreGeminiToolCallSignaturesMissesQuietly(t *testing.T) {
	replayed := []model.ToolCall{
		{ID: "call_compat_unknown_7", Function: model.FunctionCall{Name: "nope"}},
	}
	RestoreGeminiToolCallSignatures(replayed)

	if replayed[0].ThoughtSignature != "" {
		t.Fatalf("unexpected signature %q", replayed[0].ThoughtSignature)
	}
}

// nil 与空签名不应写入缓存，避免污染 key。
func TestSaveGeminiToolCallSignatureIgnoresEmpty(t *testing.T) {
	SaveGeminiToolCallSignature(nil)
	SaveGeminiToolCallSignature(&model.ToolCall{ID: "call_compat_empty_0", Function: model.FunctionCall{Name: "search"}})

	if got := RestoreGeminiThoughtSignature("call_compat_empty_0", "search"); got != "" {
		t.Fatalf("expected no cached signature, got %q", got)
	}
}
