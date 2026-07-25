package compat

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

const (
	geminiThoughtSignatureTTL        = 24 * time.Hour
	geminiThoughtSignatureMaxEntries = 10000
)

type geminiThoughtSignatureEntry struct {
	signature string
	expiresAt time.Time
}

var geminiThoughtSignatureCache = cache.New[string, geminiThoughtSignatureEntry](64)
var geminiThoughtSignatureCacheMu sync.Mutex
var geminiThoughtSignatureSaveCount uint64

// SaveGeminiThoughtSignature stores Gemini's opaque thoughtSignature without
// mutating the public tool_use ID that Anthropic clients keep in history.
func SaveGeminiThoughtSignature(toolCallID, toolName, signature string) {
	toolCallID = strings.TrimSpace(toolCallID)
	signature = strings.TrimSpace(signature)
	if toolCallID == "" || signature == "" {
		return
	}

	entry := geminiThoughtSignatureEntry{
		signature: signature,
		expiresAt: time.Now().Add(geminiThoughtSignatureTTL),
	}
	geminiThoughtSignatureCacheMu.Lock()
	defer geminiThoughtSignatureCacheMu.Unlock()
	geminiThoughtSignatureCache.Set(geminiThoughtSignatureKey(toolCallID, toolName), entry)
	geminiThoughtSignatureCache.Set(geminiThoughtSignatureKey(toolCallID, ""), entry)
	geminiThoughtSignatureSaveCount++
	if geminiThoughtSignatureSaveCount%128 == 0 || geminiThoughtSignatureCache.Len() > geminiThoughtSignatureMaxEntries {
		pruneGeminiThoughtSignaturesLocked(time.Now())
	}
}

// RestoreGeminiThoughtSignature returns a cached Gemini thoughtSignature for
// a tool_use ID previously sent to an Anthropic-compatible client.
func RestoreGeminiThoughtSignature(toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	geminiThoughtSignatureCacheMu.Lock()
	defer geminiThoughtSignatureCacheMu.Unlock()
	for _, key := range []string{
		geminiThoughtSignatureKey(toolCallID, toolName),
		geminiThoughtSignatureKey(toolCallID, ""),
	} {
		entry, ok := geminiThoughtSignatureCache.Get(key)
		if !ok {
			continue
		}
		if time.Now().After(entry.expiresAt) {
			geminiThoughtSignatureCache.Del(key)
			continue
		}
		return entry.signature
	}
	return ""
}

func pruneGeminiThoughtSignaturesLocked(now time.Time) {
	entries := geminiThoughtSignatureCache.GetAll()
	type expiringKey struct {
		key       string
		expiresAt time.Time
	}
	remaining := make([]expiringKey, 0, len(entries))
	for key, entry := range entries {
		if !now.Before(entry.expiresAt) {
			geminiThoughtSignatureCache.Del(key)
			continue
		}
		remaining = append(remaining, expiringKey{key: key, expiresAt: entry.expiresAt})
	}
	if len(remaining) <= geminiThoughtSignatureMaxEntries {
		return
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].expiresAt.Before(remaining[j].expiresAt)
	})
	for _, item := range remaining[:len(remaining)-geminiThoughtSignatureMaxEntries] {
		geminiThoughtSignatureCache.Del(item.key)
	}
}

func geminiThoughtSignatureKey(toolCallID, toolName string) string {
	return strings.TrimSpace(toolCallID) + "\x00" + strings.TrimSpace(toolName)
}
