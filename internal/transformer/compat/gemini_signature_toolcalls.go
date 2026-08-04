package compat

import (
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// SaveGeminiToolCallSignature 记住单个 tool call 携带的 Gemini thoughtSignature。
// OpenAI Chat 协议没有承载 thoughtSignature 的字段，ToolCall.ThoughtSignature 又是
// json:"-"，签名随响应发给客户端时必然被丢弃，因此网关自己按 tool call ID 存一份。
func SaveGeminiToolCallSignature(toolCall *model.ToolCall) {
	if toolCall == nil {
		return
	}
	sig := strings.TrimSpace(toolCall.GetGeminiExtensions().ThoughtSignature)
	if sig == "" {
		return
	}
	SaveGeminiThoughtSignature(toolCall.ID, toolCall.Function.Name, sig)
}

// SaveGeminiToolCallSignatures 是 SaveGeminiToolCallSignature 的批量版本。
func SaveGeminiToolCallSignatures(toolCalls []model.ToolCall) {
	for i := range toolCalls {
		SaveGeminiToolCallSignature(&toolCalls[i])
	}
}

// RestoreGeminiToolCallSignatures 为缺少签名的 tool call 回填 thoughtSignature。
// 客户端已经带回签名的条目保持原样，避免覆盖比缓存更新的值。
func RestoreGeminiToolCallSignatures(toolCalls []model.ToolCall) {
	for i := range toolCalls {
		if strings.TrimSpace(toolCalls[i].GetGeminiExtensions().ThoughtSignature) != "" {
			continue
		}
		if sig := RestoreGeminiThoughtSignature(toolCalls[i].ID, toolCalls[i].Function.Name); sig != "" {
			toolCalls[i].ThoughtSignature = sig
		}
	}
}
