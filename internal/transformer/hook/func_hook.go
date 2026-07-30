package hook

import (
	"context"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// RequestHookFunc 是用函数快速构造 RequestHook 的辅助类型，避免为简单 hook 定义完整结构体。
type RequestHookFunc struct {
	// HookName 返回给 Name()。
	HookName string
	// AppliesFn 判定适用性；为 nil 时视为「对所有格式/模型适用」。
	AppliesFn func(target model.APIFormat, modelName string) bool
	// ApplyFn 执行就地修改；为 nil 时为 no-op。
	ApplyFn func(ctx context.Context, req *model.InternalLLMRequest)
}

// Name 实现 RequestHook。
func (f RequestHookFunc) Name() string { return f.HookName }

// Applies 实现 RequestHook。AppliesFn 为 nil 时默认适用。
func (f RequestHookFunc) Applies(target model.APIFormat, modelName string) bool {
	if f.AppliesFn == nil {
		return true
	}
	return f.AppliesFn(target, modelName)
}

// Apply 实现 RequestHook。ApplyFn 为 nil 时为 no-op。
func (f RequestHookFunc) Apply(ctx context.Context, req *model.InternalLLMRequest) {
	if f.ApplyFn == nil {
		return
	}
	f.ApplyFn(ctx, req)
}
