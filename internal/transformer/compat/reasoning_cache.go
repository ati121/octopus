package compat

import (
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

const (
	reasoningContentTTL        = 24 * time.Hour
	reasoningContentMaxEntries = 10000
)

var reasoningContentCache = newReplayCache(reasoningContentTTL, reasoningContentMaxEntries)

// SaveReasoningContent 按本轮发起的 tool call ID 存档 assistant 的推理正文。
//
// DeepSeek 等 thinking 模式的上游要求重放历史时把 reasoning_content 原样带回，
// 否则整轮请求被拒（"The `reasoning_content` in the thinking mode must be passed
// back to the API."）。而 OpenAI Responses 协议只回显 reasoning 摘要与
// encrypted_content，正文在出站那一刻就丢了，客户端无从带回。
//
// 一轮推理对应一整批 tool call，无法拆分归属，因此每个 ID 都指向同一份正文；
// 回填时只取其中一个即可，见 [RestoreReasoningContent]。
func SaveReasoningContent(toolCalls []model.ToolCall, reasoning string) {
	if strings.TrimSpace(reasoning) == "" {
		return
	}

	for _, tc := range toolCalls {
		reasoningContentCache.set(tc.ID, tc.Function.Name, reasoning)
	}
}

// RestoreReasoningContent 取回某轮 assistant 的推理正文。
// 同轮所有 tool call 存的是同一份，命中任意一个即可返回。
func RestoreReasoningContent(toolCalls []model.ToolCall) string {
	for _, tc := range toolCalls {
		if reasoning := reasoningContentCache.get(tc.ID, tc.Function.Name); reasoning != "" {
			return reasoning
		}
	}

	return ""
}
