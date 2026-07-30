package hook

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestRegistryAppliesInOrder(t *testing.T) {
	r := NewRegistry()

	var order []string
	r.RegisterRequest(RequestHookFunc{
		HookName: "first",
		ApplyFn: func(_ context.Context, req *model.InternalLLMRequest) {
			order = append(order, "first")
		},
	})
	r.RegisterRequest(RequestHookFunc{
		HookName: "second",
		ApplyFn: func(_ context.Context, req *model.InternalLLMRequest) {
			order = append(order, "second")
		},
	})

	req := &model.InternalLLMRequest{Model: "gpt-4o"}
	r.ApplyRequest(context.Background(), model.APIFormatOpenAIChatCompletion, req)

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("hooks not applied in registration order: %v", order)
	}
}

func TestRegistryRespectsApplies(t *testing.T) {
	r := NewRegistry()

	applied := false
	r.RegisterRequest(RequestHookFunc{
		HookName: "gemini-only",
		AppliesFn: func(target model.APIFormat, _ string) bool {
			return target == model.APIFormatGeminiContents
		},
		ApplyFn: func(_ context.Context, req *model.InternalLLMRequest) {
			applied = true
		},
	})

	req := &model.InternalLLMRequest{Model: "gpt-4o"}
	r.ApplyRequest(context.Background(), model.APIFormatOpenAIChatCompletion, req)
	if applied {
		t.Fatal("hook applied to non-matching target format")
	}

	r.ApplyRequest(context.Background(), model.APIFormatGeminiContents, req)
	if !applied {
		t.Fatal("hook not applied to matching target format")
	}
}

func TestRegistryNilRequestSafe(t *testing.T) {
	r := NewRegistry()
	r.RegisterRequest(RequestHookFunc{
		HookName: "boom",
		ApplyFn: func(_ context.Context, req *model.InternalLLMRequest) {
			_ = req.Model // would panic on nil
		},
	})
	// Must not panic.
	r.ApplyRequest(context.Background(), model.APIFormatOpenAIChatCompletion, nil)
}

func TestRegistryNilHookIgnored(t *testing.T) {
	r := NewRegistry()
	r.RegisterRequest(nil)
	if r.RequestHookCount() != 0 {
		t.Fatalf("nil hook should be ignored, count=%d", r.RequestHookCount())
	}
}

func TestFuncHookDefaults(t *testing.T) {
	h := RequestHookFunc{HookName: "defaults"}
	if h.Name() != "defaults" {
		t.Fatalf("unexpected name %q", h.Name())
	}
	// AppliesFn nil => applies to everything.
	if !h.Applies(model.APIFormatOpenAIChatCompletion, "any") {
		t.Fatal("nil AppliesFn should default to applies=true")
	}
	// ApplyFn nil => no-op, must not panic.
	h.Apply(context.Background(), &model.InternalLLMRequest{})
}
