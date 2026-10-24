package relay

import (
	"testing"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// 缺失 usage 时保留未知用量，不把媒体编码或 JSON 当作模型输入估算。
func TestSetInternalResponseLeavesMissingUsageUnknown(t *testing.T) {
	m := NewRelayMetrics(0, "test-model", []byte(`{"messages":[{"role":"user","content":"hello"}]}`), nil)
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{}, "test-model")

	if m.Stats.InputToken != 0 {
		t.Fatalf("input token: got %d want 0", m.Stats.InputToken)
	}
	if m.BillInputTokens != nil || m.CacheReadTokens != nil || m.CacheWriteTokens != nil {
		t.Fatal("missing usage must not produce a token breakdown")
	}
	if m.Stats.OutputToken != 0 {
		t.Fatalf("output token: got %d want 0", m.Stats.OutputToken)
	}
}

// 上游只上报 output 时，输入保持 0，输出照常记录。
func TestSetInternalResponsePreservesOutputOnlyUsage(t *testing.T) {
	m := &RelayMetrics{}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{Response: transformerModel.Response{Usage: &transformerModel.Usage{PromptTokens: 0, CompletionTokens: 30}}}, "test-model")

	if m.Stats.InputToken != 0 {
		t.Fatalf("input token: got %d want 0", m.Stats.InputToken)
	}
	if m.BillInputTokens == nil || *m.BillInputTokens != 0 {
		t.Fatalf("bill input tokens: got %v want reported zero", m.BillInputTokens)
	}
	if m.Stats.OutputToken != 30 {
		t.Fatalf("output token: got %d want 30 (preserved)", m.Stats.OutputToken)
	}
}

// 上游正常上报 input 时不触发兜底（保留真实值，而非估算值）。
func TestSetInternalResponseNoFallbackWhenInputReported(t *testing.T) {
	m := &RelayMetrics{}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{Response: transformerModel.Response{Usage: &transformerModel.Usage{PromptTokens: 12, CompletionTokens: 7}}}, "test-model")

	if m.Stats.InputToken != 12 {
		t.Fatalf("input token: got %d want 12 (reported, not fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 7 {
		t.Fatalf("output token: got %d want 7", m.Stats.OutputToken)
	}
}

// 仅缓存命中（input_tokens=0 但 cache_read>0）属于已上报输入，不应被估算覆盖。
func TestSetInternalResponseNoFallbackWhenCacheOnly(t *testing.T) {
	m := &RelayMetrics{}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{Response: transformerModel.Response{Usage: &transformerModel.Usage{PromptTokens: 40, PromptTokensDetails: &transformerModel.PromptTokensDetails{CachedTokens: 40}, CompletionTokens: 5}}}, "test-model")

	if m.Stats.InputToken != 40 {
		t.Fatalf("input token: got %d want 40 (normalized cache-only input)", m.Stats.InputToken)
	}
}

func TestSetInternalResponseNormalizesAnthropicInput(t *testing.T) {
	m := &RelayMetrics{}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{Response: transformerModel.Response{Usage: &transformerModel.Usage{
		PromptTokens:        650,
		PromptTokensDetails: &transformerModel.PromptTokensDetails{CachedTokens: 500, WriteCachedTokens: 30},
		CompletionTokens:    80,
	}},
	}, "test-model")

	if m.Stats.InputToken != 650 {
		t.Fatalf("input token: got %d want 650", m.Stats.InputToken)
	}
	if m.Stats.CacheReadToken != 500 {
		t.Fatalf("cache read token: got %d want 500", m.Stats.CacheReadToken)
	}
}
