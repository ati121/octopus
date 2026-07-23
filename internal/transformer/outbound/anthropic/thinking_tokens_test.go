package anthropic

import (
	"context"
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestSupportsAdaptiveThinkingModel(t *testing.T) {
	tests := map[string]bool{
		"claude-opus-4-8":            true,
		"claude-opus-4.7":            true,
		"claude-4-6-sonnet":          true,
		"claude-sonnet-5":            true,
		"claude-opus-4-5-20251101":   false,
		"claude-3-5-sonnet-20241022": false,
		"gpt-5.5":                    false,
	}
	for modelName, want := range tests {
		t.Run(modelName, func(t *testing.T) {
			if got := supportsAdaptiveThinkingModel(modelName); got != want {
				t.Fatalf("supportsAdaptiveThinkingModel(%q) = %t, want %t", modelName, got, want)
			}
		})
	}
}

func TestConvertToAnthropicRequestUsesAdaptiveThinkingForClaude48(t *testing.T) {
	req, err := (&openaiInbound.ResponseInbound{}).TransformRequest(context.Background(), []byte(`{
		"model":"claude-opus-4-8",
		"input":"hello",
		"reasoning":{"effort":"medium","summary":"auto"}
	}`))
	if err != nil {
		t.Fatalf("transform Responses request: %v", err)
	}

	out := convertToAnthropicRequest(req)
	if out.Thinking == nil || out.Thinking.Type != anthropicModel.ThinkingTypeAdaptive {
		t.Fatalf("expected adaptive thinking, got %+v", out.Thinking)
	}
	if out.Thinking.BudgetTokens != nil {
		t.Fatalf("adaptive thinking must not contain budget_tokens, got %d", *out.Thinking.BudgetTokens)
	}
	if out.OutputConfig == nil || out.OutputConfig.Effort != anthropicModel.EffortMedium {
		t.Fatalf("expected output_config.effort=medium, got %+v", out.OutputConfig)
	}
}

func TestConvertToAnthropicRequestExplicitBudgetKeepsClassicThinking(t *testing.T) {
	budget := int64(4096)
	req := &model.InternalLLMRequest{
		Model:           "claude-opus-4-8",
		ReasoningEffort: anthropicModel.EffortMedium,
		ReasoningBudget: &budget,
	}

	out := convertToAnthropicRequest(req)
	if out.Thinking == nil || out.Thinking.Type != anthropicModel.ThinkingTypeEnabled {
		t.Fatalf("explicit reasoning budget should use classic thinking, got %+v", out.Thinking)
	}
	if out.Thinking.BudgetTokens == nil || *out.Thinking.BudgetTokens != budget {
		t.Fatalf("budget_tokens = %v, want %d", out.Thinking.BudgetTokens, budget)
	}
	if out.MaxTokens <= *out.Thinking.BudgetTokens {
		t.Fatalf("budget_tokens must be less than max_tokens: budget=%d max=%d", *out.Thinking.BudgetTokens, out.MaxTokens)
	}
}

func TestConvertToAnthropicRequestCoordinatesClassicThinkingTokens(t *testing.T) {
	tests := []struct {
		name                string
		effort              string
		budget              *int64
		maxTokens           *int64
		maxCompletionTokens *int64
		wantBudget          int64
		wantMax             int64
	}{
		{name: "default medium", effort: anthropicModel.EffortMedium, wantBudget: 8192, wantMax: 16384},
		{name: "default high", effort: anthropicModel.EffortHigh, wantBudget: 32768, wantMax: 32769},
		{name: "equal explicit max clamps budget", effort: anthropicModel.EffortMedium, maxTokens: int64Pointer(8192), wantBudget: 8191, wantMax: 8192},
		{name: "max completion clamps budget", effort: anthropicModel.EffortMedium, maxCompletionTokens: int64Pointer(4096), wantBudget: 4095, wantMax: 4096},
		{name: "tiny explicit max becomes minimum valid pair", effort: anthropicModel.EffortMedium, maxTokens: int64Pointer(1), wantBudget: 1024, wantMax: 1025},
		{name: "small explicit budget is raised to minimum", effort: anthropicModel.EffortLow, budget: int64Pointer(512), maxTokens: int64Pointer(2048), wantBudget: 1024, wantMax: 2048},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{
				Model:               "claude-3-5-sonnet",
				ReasoningEffort:     tt.effort,
				ReasoningBudget:     tt.budget,
				MaxTokens:           tt.maxTokens,
				MaxCompletionTokens: tt.maxCompletionTokens,
			}
			out := convertToAnthropicRequest(req)
			if out.Thinking == nil || out.Thinking.Type != anthropicModel.ThinkingTypeEnabled || out.Thinking.BudgetTokens == nil {
				t.Fatalf("expected classic thinking with budget, got %+v", out.Thinking)
			}
			if got := *out.Thinking.BudgetTokens; got != tt.wantBudget {
				t.Fatalf("budget_tokens = %d, want %d", got, tt.wantBudget)
			}
			if out.MaxTokens != tt.wantMax {
				t.Fatalf("max_tokens = %d, want %d", out.MaxTokens, tt.wantMax)
			}
			if *out.Thinking.BudgetTokens >= out.MaxTokens {
				t.Fatalf("invalid classic thinking pair: budget=%d max=%d", *out.Thinking.BudgetTokens, out.MaxTokens)
			}
		})
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
