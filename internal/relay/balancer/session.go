package balancer

import (
	"fmt"
	"sync"
	"time"
)

const (
	maxStickySessionEntries = 10000
	maxStickySessionAge     = 24 * time.Hour
)

// SessionEntry 会话保持条目
type SessionEntry struct {
	ChannelID    int
	ChannelKeyID int
	Timestamp    time.Time
}

type stickySessionStore struct {
	mu        sync.Mutex
	entries   map[string]SessionEntry
	lastPrune time.Time
}

var globalSession = newStickySessionStore()

func newStickySessionStore() *stickySessionStore {
	return &stickySessionStore{entries: make(map[string]SessionEntry)}
}

// sessionKey 生成会话键：apiKeyID:requestModel
func sessionKey(apiKeyID int, requestModel string) string {
	return fmt.Sprintf("%d:%s", apiKeyID, requestModel)
}

// GetSticky 获取粘性通道（ttl 内有效）
// ttl 由 Group.SessionKeepTime 决定，返回 nil 表示无有效会话
func GetSticky(apiKeyID int, requestModel string, ttl time.Duration) *SessionEntry {
	key := sessionKey(apiKeyID, requestModel)
	now := time.Now()
	globalSession.mu.Lock()
	entry, ok := globalSession.entries[key]
	if !ok {
		globalSession.mu.Unlock()
		return nil
	}
	if ttl <= 0 || now.Sub(entry.Timestamp) > ttl {
		delete(globalSession.entries, key)
		globalSession.mu.Unlock()
		return nil
	}
	globalSession.mu.Unlock()
	cloned := entry
	return &cloned
}

// SetSticky 写入/更新粘性记录
func SetSticky(apiKeyID int, requestModel string, channelID, keyID int) {
	key := sessionKey(apiKeyID, requestModel)
	now := time.Now()
	globalSession.mu.Lock()
	if globalSession.lastPrune.IsZero() || now.Sub(globalSession.lastPrune) >= time.Minute {
		globalSession.pruneLocked(now)
		globalSession.lastPrune = now
	}
	globalSession.entries[key] = SessionEntry{
		ChannelID:    channelID,
		ChannelKeyID: keyID,
		Timestamp:    now,
	}
	globalSession.evictLocked()
	globalSession.mu.Unlock()
}

func DeleteSticky(apiKeyID int, requestModel string) {
	globalSession.mu.Lock()
	delete(globalSession.entries, sessionKey(apiKeyID, requestModel))
	globalSession.mu.Unlock()
}

func resetStickyByChannel(channelID int) {
	globalSession.mu.Lock()
	for key, entry := range globalSession.entries {
		if entry.ChannelID == channelID {
			delete(globalSession.entries, key)
		}
	}
	globalSession.mu.Unlock()
}

func (s *stickySessionStore) pruneLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.Sub(entry.Timestamp) > maxStickySessionAge {
			delete(s.entries, key)
		}
	}
}

func (s *stickySessionStore) evictLocked() {
	for len(s.entries) > maxStickySessionEntries {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range s.entries {
			if oldestKey == "" || entry.Timestamp.Before(oldest) {
				oldestKey = key
				oldest = entry.Timestamp
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.entries, oldestKey)
	}
}

func resetStickySessions() {
	globalSession.mu.Lock()
	globalSession.entries = make(map[string]SessionEntry)
	globalSession.lastPrune = time.Time{}
	globalSession.mu.Unlock()
}
