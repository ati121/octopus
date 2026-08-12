package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func int64Ptr(v int64) *int64 { return &v }

// Chat 客户端（如 Hermes）发的是 max_tokens；Responses API 只认
// max_output_tokens。转换必须回落，否则长度上限被静默丢弃。
func TestConvertToResponsesRequestFallsBackMaxTokensToMaxOutputTokens(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:    "deepseek-v4-flash",
		MaxTokens: int64Ptr(2048),
	}
	out := ConvertToResponsesRequest(req)
	if out.MaxOutputTokens == nil || *out.MaxOutputTokens != 2048 {
		t.Fatalf("expected max_tokens to fall back to max_output_tokens, got %+v", out.MaxOutputTokens)
	}

	// max_completion_tokens 优先于 max_tokens。
	req = &model.InternalLLMRequest{
		Model:             "deepseek-v4-flash",
		MaxTokens:         int64Ptr(2048),
		MaxCompletionTokens: int64Ptr(4096),
	}
	out = ConvertToResponsesRequest(req)
	if out.MaxOutputTokens == nil || *out.MaxOutputTokens != 4096 {
		t.Fatalf("expected max_completion_tokens to take precedence, got %+v", out.MaxOutputTokens)
	}

	// 两者都为空时不长字段。
	req = &model.InternalLLMRequest{Model: "deepseek-v4-flash"}
	out = ConvertToResponsesRequest(req)
	if out.MaxOutputTokens != nil {
		t.Fatalf("expected nil max_output_tokens when both unset, got %+v", out.MaxOutputTokens)
	}
}

// Chat 请求的 stream_options.include_usage 必须映射到 Responses 的同名字段，
// 保证请求方要求的最终 usage 分片不因跨格式转换丢失。
func TestConvertToResponsesRequestMapsChatStreamOptionsIncludeUsage(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:         "deepseek-v4-flash",
		StreamOptions: &model.StreamOptions{IncludeUsage: true},
	}
	out := ConvertToResponsesRequest(req)
	if out.StreamOptions == nil {
		t.Fatalf("expected stream_options to be mapped from chat include_usage")
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.StreamOptions, &parsed); err != nil {
		t.Fatalf("stream_options must be valid JSON, got %s: %v", out.StreamOptions, err)
	}
	if parsed["include_usage"] != true {
		t.Fatalf("expected include_usage=true, got %#v", parsed)
	}

	// IncludeUsage=false 不映射。
	req = &model.InternalLLMRequest{
		Model:         "deepseek-v4-flash",
		StreamOptions: &model.StreamOptions{IncludeUsage: false},
	}
	out = ConvertToResponsesRequest(req)
	if out.StreamOptions != nil {
		t.Fatalf("expected no stream_options for include_usage=false, got %s", out.StreamOptions)
	}

	// Responses 入站自带的 stream_options 优先于 chat 映射。
	req = &model.InternalLLMRequest{Model: "deepseek-v4-flash"}
	req.SetOpenAIResponsesOptions(model.OpenAIResponsesOptions{
		StreamOptions: json.RawMessage(`{"include_usage":true,"custom":1}`),
	})
	out = ConvertToResponsesRequest(req)
	if out.StreamOptions == nil || !json.Valid(out.StreamOptions) {
		t.Fatalf("expected responses-provided stream_options preserved, got %s", out.StreamOptions)
	}
}
