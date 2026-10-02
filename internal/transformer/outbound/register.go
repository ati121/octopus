package outbound

import (
	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
	// OutboundTypeCodex 走 ChatGPT Codex 线路：协议与 OpenAI Responses 基本一致，
	// 仅额外注入 Codex 特征请求头。追加在枚举末尾以保持已持久化的类型值稳定。
	OutboundTypeCodex
	// OutboundTypeRerank 追加在末尾，避免改变已持久化渠道类型的数值。
	OutboundTypeRerank
	// OutboundTypeSystemOne 对接 TypeSafe System One（Jev 模型）的 /v1/systemone，
	// 同样追加在末尾，保持已持久化的类型值稳定。
	OutboundTypeSystemOne
)

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
}

// RerankChannelTypes 定义支持 rerank 请求的 channel 类型集合。
var RerankChannelTypes = map[OutboundType]bool{
	OutboundTypeRerank: true,
}

// SystemOneChannelTypes 定义支持 System One 请求的 channel 类型集合。
var SystemOneChannelTypes = map[OutboundType]bool{
	OutboundTypeSystemOne: true,
}

// ChatChannelTypes 定义支持 chat 请求的 channel 类型集合
var ChatChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIChat:     true,
	OutboundTypeOpenAIResponse: true,
	OutboundTypeAnthropic:      true,
	OutboundTypeGemini:         true,
	OutboundTypeVolcengine:     true,
	OutboundTypeCodex:          true,
}

// outboundAPIFormats 定义每种出站 channel 类型对应的 provider APIFormat。
// 供 hook 机制识别「本次实际路由到的目标出站格式」，以决定哪些 hook 适用。
var outboundAPIFormats = map[OutboundType]model.APIFormat{
	OutboundTypeOpenAIChat:      model.APIFormatOpenAIChatCompletion,
	OutboundTypeOpenAIResponse:  model.APIFormatOpenAIResponse,
	OutboundTypeOpenAIEmbedding: model.APIFormatOpenAIEmbedding,
	OutboundTypeAnthropic:       model.APIFormatAnthropicMessage,
	OutboundTypeGemini:          model.APIFormatGeminiContents,
	// 豆包协议由 AxonHub 维护，使用 Chat Completions。
	OutboundTypeVolcengine: model.APIFormatOpenAIChatCompletion,
	// Codex 走 OpenAI Responses 线路（内部内嵌 openai.ResponseOutbound，仅额外注入特征头）。
	OutboundTypeCodex:     model.APIFormatOpenAIResponse,
	OutboundTypeRerank:    model.APIFormatRerank,
	OutboundTypeSystemOne: model.APIFormatSystemOne,
}

// APIFormatOf 返回出站 channel 类型对应的 provider APIFormat。
// 未知类型返回空字符串。
func APIFormatOf(channelType OutboundType) model.APIFormat {
	return outboundAPIFormats[channelType]
}

// IsEmbeddingChannelType 判断 channel 类型是否支持 embedding 请求
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return EmbeddingChannelTypes[channelType]
}

// IsRerankChannelType 判断 channel 类型是否支持 rerank 请求。
func IsRerankChannelType(channelType OutboundType) bool {
	return RerankChannelTypes[channelType]
}

// IsSystemOneChannelType 判断 channel 类型是否支持 System One 请求。
func IsSystemOneChannelType(channelType OutboundType) bool {
	return SystemOneChannelTypes[channelType]
}

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

// IsValidChannelType 判断 channel 类型是否注册了出站适配器
func IsValidChannelType(channelType OutboundType) bool {
	_, ok := outboundFactories[channelType]
	return ok
}

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return axon.NewOutbound(0) },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return axon.NewOutbound(1) },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return axon.NewOutbound(5) },
	OutboundTypeAnthropic:       func() model.Outbound { return axon.NewOutbound(2) },
	OutboundTypeGemini:          func() model.Outbound { return axon.NewOutbound(3) },
	OutboundTypeVolcengine:      func() model.Outbound { return axon.NewOutbound(4) },
	OutboundTypeCodex:           func() model.Outbound { return axon.NewOutbound(6) },
	OutboundTypeRerank:          func() model.Outbound { return axon.NewOutbound(7) },
	OutboundTypeSystemOne:       func() model.Outbound { return axon.NewOutbound(8) },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}
