package outbound

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
	outAnthropic "github.com/bestruirui/octopus/internal/transformer/outbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/outbound/codex"
	"github.com/bestruirui/octopus/internal/transformer/outbound/gemini"
	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/outbound/volcengine"
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
)

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
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
	// Volcengine 走 OpenAI Responses 线路（内部内嵌 openai.ResponseOutbound）。
	OutboundTypeVolcengine: model.APIFormatOpenAIResponse,
	// Codex 走 OpenAI Responses 线路（内部内嵌 openai.ResponseOutbound，仅额外注入特征头）。
	OutboundTypeCodex: model.APIFormatOpenAIResponse,
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

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return &openai.ChatOutbound{} },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return &openai.ResponseOutbound{} },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return &openai.EmbeddingOutbound{} },
	OutboundTypeAnthropic:       func() model.Outbound { return &outAnthropic.MessageOutbound{} },
	OutboundTypeGemini:          func() model.Outbound { return &gemini.MessagesOutbound{} },
	OutboundTypeVolcengine:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
	OutboundTypeCodex:           func() model.Outbound { return &codex.ResponseOutbound{} },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}
