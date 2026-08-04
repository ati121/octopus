package compat

import (
	"time"
)

const (
	geminiThoughtSignatureTTL        = 24 * time.Hour
	geminiThoughtSignatureMaxEntries = 10000
)

var geminiThoughtSignatureCache = newReplayCache(geminiThoughtSignatureTTL, geminiThoughtSignatureMaxEntries)

// SaveGeminiThoughtSignature stores Gemini's opaque thoughtSignature without
// mutating the public tool_use ID that Anthropic clients keep in history.
func SaveGeminiThoughtSignature(toolCallID, toolName, signature string) {
	geminiThoughtSignatureCache.set(toolCallID, toolName, signature)
}

// RestoreGeminiThoughtSignature returns a cached Gemini thoughtSignature for
// a tool_use ID previously sent to an Anthropic-compatible client.
func RestoreGeminiThoughtSignature(toolCallID, toolName string) string {
	return geminiThoughtSignatureCache.get(toolCallID, toolName)
}
