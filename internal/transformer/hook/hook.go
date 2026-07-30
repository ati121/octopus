// Package hook 提供出站转换前后的可插拔调整机制（middleware / hook）。
//
// 设计目标：把「按模型/渠道做的协议 quirk 修补」从各 outbound 转换器主干里分离出来，
// 例如「某些模型不支持 reasoning，转换前需要剥离」这类逻辑，避免散落在 TransformRequest 内部。
//
// 分层说明：
//   - hook 作用在「内部通用请求 IR」这一层，即 outbound.TransformRequest 把 IR 转成 provider
//     具体格式「之前」。它与 relay 层的 applyParamOverride（作用于最终 http.Request body 字节）
//     是互补的两层：前者语义清晰、类型安全；后者面向渠道级 JSON 覆盖。
//
// 并发与副作用约定（重要）：
//   - 同一个 *model.InternalLLMRequest 在多次重试 / 换渠道之间是共享的（见 relay.relayRequest）。
//     现有 outbound 已存在原地修改 IR 的模式（NormalizeMessages、EnforceMessageAlternation），
//     hook 遵循同样约定：实现必须「幂等」（同一 IR 多次调用结果一致），且不得引入跨请求的
//     全局可变副作用。
//   - Registry 自身可被多 goroutine 并发读取（每个请求一条），注册通常只在启动阶段完成。
package hook

import (
	"context"
	"sync"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// RequestHook 在 IR 请求交给出站转换器之前，对其做规范化 / 修补调整。
type RequestHook interface {
	// Name 返回 hook 名称，用于日志与调试。
	Name() string

	// Applies 判断该 hook 是否适用于目标出站格式与模型名。
	// target 为出站 provider 的 APIFormat；modelName 为本次实际路由到的模型名。
	Applies(target model.APIFormat, modelName string) bool

	// Apply 就地修改 IR 请求。实现必须幂等且无跨请求副作用。
	Apply(ctx context.Context, req *model.InternalLLMRequest)
}

// Registry 管理一组 RequestHook，按注册顺序应用。
type Registry struct {
	mu           sync.RWMutex
	requestHooks []RequestHook
}

// NewRegistry 构造一个空的 hook 注册表。
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterRequest 追加一个请求 hook。通常在启动阶段调用。
func (r *Registry) RegisterRequest(h RequestHook) {
	if h == nil {
		return
	}
	r.mu.Lock()
	r.requestHooks = append(r.requestHooks, h)
	r.mu.Unlock()
}

// ApplyRequest 对 IR 请求依次应用所有适用的请求 hook。
// target 为目标出站格式，modelName 为实际路由模型名。
// req 为 nil 时安全返回。
func (r *Registry) ApplyRequest(ctx context.Context, target model.APIFormat, req *model.InternalLLMRequest) {
	if req == nil {
		return
	}
	r.mu.RLock()
	hooks := r.requestHooks
	r.mu.RUnlock()

	modelName := req.Model
	for _, h := range hooks {
		if h == nil || !h.Applies(target, modelName) {
			continue
		}
		h.Apply(ctx, req)
	}
}

// RequestHookCount 返回已注册的请求 hook 数量，主要用于测试与诊断。
func (r *Registry) RequestHookCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.requestHooks)
}

var defaultRegistry = NewRegistry()

// Default 返回包级共享注册表。
func Default() *Registry { return defaultRegistry }

// RegisterRequest 在默认注册表上追加请求 hook。
func RegisterRequest(h RequestHook) { defaultRegistry.RegisterRequest(h) }

// ApplyRequest 使用默认注册表对 IR 请求应用请求 hook。
func ApplyRequest(ctx context.Context, target model.APIFormat, req *model.InternalLLMRequest) {
	defaultRegistry.ApplyRequest(ctx, target, req)
}
