// Package codex 提供 ChatGPT Codex 出站转换器。
//
// 协议关系：Codex 后端的请求/响应协议与 OpenAI Responses API 基本一致，
// 因此本转换器内嵌 openai.ResponseOutbound，复用其全部字段映射、流式事件拆分、
// passthrough 等能力，仅在构建 HTTP 请求时额外注入 Codex 特征请求头。
//
// 与 volcengine 出站的区别：volcengine 需要改写请求体（thinking / reasoning 字段），
// 而 Codex 不改协议字段，只在传输层补充 header，所以这里对 body 完全透传委托。
//
// 认证与 baseURL：沿用 octopus 既有的渠道配置模型——key、baseURL 由用户在渠道里配置，
// 本转换器不做 OAuth 或 token 管理。特征 header 使用合理默认值，可被渠道级
// CustomHeader 覆盖（见 relay.copyHeaders 的应用顺序）。
package codex

import (
	"context"
	"net/http"
	"net/url"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

// Codex 特征请求头。这些值模拟官方 Codex 客户端的传输特征，部分上游会据此校验。
// 均为「仅在缺省时设置」，因此渠道的 CustomHeader 可以覆盖它们。
const (
	headerOriginator     = "originator"
	headerOpenAIBeta     = "OpenAI-Beta"
	defaultOriginator    = "codex_cli_rs"
	defaultOpenAIBetaVal = "responses=experimental"
)

// ResponseOutbound 是 Codex 出站转换器，内嵌 OpenAI Responses 出站实现。
type ResponseOutbound struct {
	inner openai.ResponseOutbound
}

// 编译期接口断言：确保 Codex 出站与 openai.ResponseOutbound 一样满足全部可选接口，
// 不会在 relay 的类型断言路径（流式事件 / passthrough）上退化。
var (
	_ model.Outbound                       = (*ResponseOutbound)(nil)
	_ model.OutboundStreamEventTransformer = (*ResponseOutbound)(nil)
	_ model.PassthroughCapable             = (*ResponseOutbound)(nil)
)

// applyCodexHeaders 注入 Codex 特征头。仅在对应 header 缺省时设置，
// 以便渠道 CustomHeader / 客户端透传头覆盖。
func applyCodexHeaders(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header.Get(headerOriginator) == "" {
		req.Header.Set(headerOriginator, defaultOriginator)
	}
	if req.Header.Get(headerOpenAIBeta) == "" {
		req.Header.Set(headerOpenAIBeta, defaultOpenAIBetaVal)
	}
}

// TransformRequest 委托内嵌的 Responses 出站构建请求，再注入 Codex 特征头。
func (o *ResponseOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	req, err := o.inner.TransformRequest(ctx, request, baseUrl, key)
	if err != nil {
		return nil, err
	}
	applyCodexHeaders(req)
	return req, nil
}

// TransformRequestRaw 覆盖同格式直通（OpenAI Responses → Codex）路径：
// 委托内嵌实现保留原始 body 与字节稳定性，再注入 Codex 特征头。
func (o *ResponseOutbound) TransformRequestRaw(ctx context.Context, rawBody []byte, modelName, baseUrl, key string, query url.Values) (*http.Request, error) {
	req, err := o.inner.TransformRequestRaw(ctx, rawBody, modelName, baseUrl, key, query)
	if err != nil {
		return nil, err
	}
	applyCodexHeaders(req)
	return req, nil
}

// TransformResponse 纯委托。
func (o *ResponseOutbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	return o.inner.TransformResponse(ctx, response)
}

// TransformStream 纯委托。
func (o *ResponseOutbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	return o.inner.TransformStream(ctx, eventData)
}

// TransformStreamEvent 纯委托，保持与 Responses 相同的显式流式事件能力。
func (o *ResponseOutbound) TransformStreamEvent(ctx context.Context, eventData []byte) ([]model.StreamEvent, error) {
	return o.inner.TransformStreamEvent(ctx, eventData)
}

// CanPassthrough 纯委托：仅当入站也是 OpenAI Responses 格式时允许字节级直通。
func (o *ResponseOutbound) CanPassthrough(inboundFormat model.APIFormat) bool {
	return o.inner.CanPassthrough(inboundFormat)
}

// PassthroughConfig 纯委托。
func (o *ResponseOutbound) PassthroughConfig() model.PassthroughConfig {
	return o.inner.PassthroughConfig()
}
