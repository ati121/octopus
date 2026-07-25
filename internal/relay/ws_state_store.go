package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

const (
	wsConversationStoreMaxEntries    = 512
	wsConversationStoreMaxBytes      = 64 * 1024 * 1024
	wsConversationStoreSweepInterval = 5 * time.Minute
)

type wsConversationStateEntry struct {
	state        *wsConversationState
	expiresAt    time.Time
	updatedAt    time.Time
	size         int64
	responseKeys []string
}

type boundedWSConversationStore struct {
	mu            sync.Mutex
	entries       map[string]*wsConversationStateEntry
	responseIndex map[string]string
	totalBytes    int64
}

var wsConversationStore = newBoundedWSConversationStore()

func init() {
	go func() {
		ticker := time.NewTicker(wsConversationStoreSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			wsConversationStore.sweepExpired(time.Now())
		}
	}()
}

func newBoundedWSConversationStore() *boundedWSConversationStore {
	return &boundedWSConversationStore{
		entries:       make(map[string]*wsConversationStateEntry),
		responseIndex: make(map[string]string),
	}
}

func wsConversationStateKey(apiKeyID int, requestModel, downstreamSessionID string) string {
	return fmt.Sprintf("%d:%s:session:%s", apiKeyID, strings.TrimSpace(requestModel), strings.TrimSpace(downstreamSessionID))
}

func wsConversationResponseKey(apiKeyID int, requestModel, responseID string) string {
	return fmt.Sprintf("%d:%s:response:%s", apiKeyID, strings.TrimSpace(requestModel), strings.TrimSpace(responseID))
}

// loadWSConversationState accepts either the original downstream session ID or
// a previous_response_id. The latter makes continuation recovery work after a
// client reconnects and receives a new server-side WebSocket session ID.
func loadWSConversationState(apiKeyID int, requestModel, sessionOrResponseID string) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	sessionOrResponseID = strings.TrimSpace(sessionOrResponseID)
	if requestModel == "" || sessionOrResponseID == "" {
		return nil
	}

	now := time.Now()
	wsConversationStore.mu.Lock()
	wsConversationStore.sweepExpiredLocked(now)
	sessionKey := wsConversationStateKey(apiKeyID, requestModel, sessionOrResponseID)
	entry := wsConversationStore.entries[sessionKey]
	if entry == nil {
		if indexedKey := wsConversationStore.responseIndex[wsConversationResponseKey(apiKeyID, requestModel, sessionOrResponseID)]; indexedKey != "" {
			sessionKey = indexedKey
			entry = wsConversationStore.entries[sessionKey]
		}
	}
	wsConversationStore.mu.Unlock()
	if entry == nil || entry.state == nil {
		return nil
	}
	return cloneWSConversationState(entry.state)
}

func storeWSConversationState(apiKeyID int, requestModel string, state *wsConversationState, ttl time.Duration) {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID := ""
	if state != nil {
		downstreamSessionID = strings.TrimSpace(state.DownstreamSessionID)
	}
	if requestModel == "" || state == nil || downstreamSessionID == "" {
		return
	}
	if ttl <= 0 {
		ttl = wsClientMaxAge
	}

	cloned := cloneWSConversationState(state)
	if cloned == nil {
		return
	}
	cloned.RequestModel = requestModel
	cloned.limitRetainedHistory()

	now := time.Now()
	sessionKey := wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID)
	entry := &wsConversationStateEntry{
		state:     cloned,
		expiresAt: now.Add(ttl),
		updatedAt: now,
		size:      estimateWSConversationStateSize(cloned),
	}
	entry.responseKeys = wsConversationResponseKeys(apiKeyID, requestModel, cloned)

	wsConversationStore.mu.Lock()
	defer wsConversationStore.mu.Unlock()
	wsConversationStore.sweepExpiredLocked(now)
	wsConversationStore.removeLocked(sessionKey)
	wsConversationStore.entries[sessionKey] = entry
	wsConversationStore.totalBytes += entry.size
	for _, responseKey := range entry.responseKeys {
		wsConversationStore.responseIndex[responseKey] = sessionKey
	}
	wsConversationStore.evictLocked()
}

func deleteWSConversationState(apiKeyID int, requestModel, downstreamSessionID string) {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID = strings.TrimSpace(downstreamSessionID)
	if requestModel == "" || downstreamSessionID == "" {
		return
	}
	wsConversationStore.mu.Lock()
	wsConversationStore.removeLocked(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID))
	wsConversationStore.mu.Unlock()
}

func wsConversationResponseKeys(apiKeyID int, requestModel string, state *wsConversationState) []string {
	if state == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(state.ReplayAliases)+1)
	result := make([]string, 0, len(state.ReplayAliases)+1)
	appendID := func(responseID string) {
		responseID = strings.TrimSpace(responseID)
		if responseID == "" {
			return
		}
		key := wsConversationResponseKey(apiKeyID, requestModel, responseID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	appendID(state.LastResponseID)
	for _, alias := range state.ReplayAliases {
		appendID(alias)
	}
	return result
}

func estimateWSConversationStateSize(state *wsConversationState) int64 {
	if state == nil {
		return 0
	}
	size := len(state.DownstreamSessionID) + len(state.RequestModel) + len(state.LastResponseID) + len(state.ReplayWindowItems)
	for _, alias := range state.ReplayAliases {
		size += len(alias)
	}
	for _, message := range state.Transcript {
		if encoded, err := json.Marshal(message); err == nil {
			size += len(encoded)
		}
	}
	return int64(size)
}

func (s *boundedWSConversationStore) sweepExpired(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sweepExpiredLocked(now)
	s.mu.Unlock()
}

func (s *boundedWSConversationStore) sweepExpiredLocked(now time.Time) {
	for key, entry := range s.entries {
		if entry == nil || entry.state == nil || (!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
			s.removeLocked(key)
		}
	}
}

func (s *boundedWSConversationStore) evictLocked() {
	for len(s.entries) > wsConversationStoreMaxEntries || s.totalBytes > wsConversationStoreMaxBytes {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range s.entries {
			if entry == nil || oldestKey == "" || entry.updatedAt.Before(oldest) {
				oldestKey = key
				if entry != nil {
					oldest = entry.updatedAt
				}
			}
		}
		if oldestKey == "" {
			return
		}
		s.removeLocked(oldestKey)
	}
}

func (s *boundedWSConversationStore) removeLocked(sessionKey string) {
	entry := s.entries[sessionKey]
	if entry == nil {
		return
	}
	delete(s.entries, sessionKey)
	s.totalBytes -= entry.size
	if s.totalBytes < 0 {
		s.totalBytes = 0
	}
	for _, responseKey := range entry.responseKeys {
		if s.responseIndex[responseKey] == sessionKey {
			delete(s.responseIndex, responseKey)
		}
	}
}

func resolveWSConversationState(apiKeyID int, requestModel string, localState *wsConversationState, allowStoredRestore bool, sessionOrResponseID string) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	sessionOrResponseID = strings.TrimSpace(sessionOrResponseID)
	if requestModel == "" {
		return localState
	}
	if localState != nil && localState.MatchesRequestModel(requestModel) {
		return localState
	}
	if !allowStoredRestore {
		return nil
	}
	return loadWSConversationState(apiKeyID, requestModel, sessionOrResponseID)
}

func wsConversationStateToSticky(state *wsConversationState) *balancer.SessionEntry {
	if state == nil || state.ChannelID <= 0 {
		return nil
	}
	return &balancer.SessionEntry{
		ChannelID:    state.ChannelID,
		ChannelKeyID: state.ChannelKeyID,
		Timestamp:    time.Now(),
	}
}

func wsConversationStateTTL(sessionKeepTimeSec int) time.Duration {
	if sessionKeepTimeSec <= 0 {
		return wsClientMaxAge
	}
	ttl := time.Duration(sessionKeepTimeSec) * time.Second
	if ttl > wsClientMaxAge {
		return wsClientMaxAge
	}
	return ttl
}

func resetWSConversationStateStore() {
	wsConversationStore.mu.Lock()
	wsConversationStore.entries = make(map[string]*wsConversationStateEntry)
	wsConversationStore.responseIndex = make(map[string]string)
	wsConversationStore.totalBytes = 0
	wsConversationStore.mu.Unlock()
}
