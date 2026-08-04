package compat

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

// replayCache 是「网关代客户端保管、重放时回填」这类补偿机制的公共载体。
//
// 有些 provider 要求把上一轮 assistant 的某项数据原样回传（Gemini 3 的
// thoughtSignature、DeepSeek thinking 模式的 reasoning_content），但客户端所用
// 协议往往没有承载它的字段，出站即丢失，下一轮重放就缺料被上游拒绝。网关按
// tool call ID 自己存一份，重放时补回去，客户端无需感知。
//
// 键一律是 tool call ID + tool name，同时额外写一条不带 tool name 的兜底键 ——
// 回填时 tool name 可能对不上（客户端改名）或压根拿不到。
type replayCache struct {
	mu         sync.Mutex
	entries    cache.Cache[string, replayEntry]
	ttl        time.Duration
	maxEntries int
	saveCount  uint64
}

type replayEntry struct {
	value     string
	expiresAt time.Time
}

func newReplayCache(ttl time.Duration, maxEntries int) *replayCache {
	return &replayCache{
		entries:    cache.New[string, replayEntry](64),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

// set 写入一条记录。toolCallID 或 value 为空时静默跳过。
func (c *replayCache) set(toolCallID, toolName, value string) {
	toolCallID = strings.TrimSpace(toolCallID)
	value = strings.TrimSpace(value)
	if toolCallID == "" || value == "" {
		return
	}

	entry := replayEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries.Set(replayCacheKey(toolCallID, toolName), entry)
	c.entries.Set(replayCacheKey(toolCallID, ""), entry)
	c.saveCount++
	if c.saveCount%128 == 0 || c.entries.Len() > c.maxEntries {
		c.pruneLocked(time.Now())
	}
}

// get 依次尝试「带 tool name」和「不带 tool name」两个键，命中过期条目顺手删掉。
func (c *replayCache) get(toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range []string{
		replayCacheKey(toolCallID, toolName),
		replayCacheKey(toolCallID, ""),
	} {
		entry, ok := c.entries.Get(key)
		if !ok {
			continue
		}
		if time.Now().After(entry.expiresAt) {
			c.entries.Del(key)
			continue
		}

		return entry.value
	}

	return ""
}

// pruneLocked 先清过期条目，仍然超量时按到期时间从早到晚淘汰。
func (c *replayCache) pruneLocked(now time.Time) {
	entries := c.entries.GetAll()

	type expiringKey struct {
		key       string
		expiresAt time.Time
	}

	remaining := make([]expiringKey, 0, len(entries))
	for key, entry := range entries {
		if !now.Before(entry.expiresAt) {
			c.entries.Del(key)
			continue
		}
		remaining = append(remaining, expiringKey{key: key, expiresAt: entry.expiresAt})
	}

	if len(remaining) <= c.maxEntries {
		return
	}

	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].expiresAt.Before(remaining[j].expiresAt)
	})
	for _, item := range remaining[:len(remaining)-c.maxEntries] {
		c.entries.Del(item.key)
	}
}

func replayCacheKey(toolCallID, toolName string) string {
	return strings.TrimSpace(toolCallID) + "\x00" + strings.TrimSpace(toolName)
}
