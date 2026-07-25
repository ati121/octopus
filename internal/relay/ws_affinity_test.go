package relay

import (
	"fmt"
	"testing"
	"time"
)

func TestWSAffinityHotCacheIsBoundedAndUsesLRU(t *testing.T) {
	cache := newWSAffinityHotCache(3)
	expiresAt := time.Now().Add(time.Hour)
	for i := 1; i <= 3; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), wsAffinityEntry{ChannelID: i, ExpiresAt: expiresAt})
	}
	if _, ok := cache.Get("key-1", time.Now()); !ok {
		t.Fatal("expected key-1 to be cached")
	}
	cache.Set("key-4", wsAffinityEntry{ChannelID: 4, ExpiresAt: expiresAt})

	if got := cache.Len(); got != 3 {
		t.Fatalf("expected cache size 3, got %d", got)
	}
	if _, ok := cache.Get("key-2", time.Now()); ok {
		t.Fatal("expected least recently used key-2 to be evicted")
	}
	if _, ok := cache.Get("key-1", time.Now()); !ok {
		t.Fatal("expected recently used key-1 to remain")
	}
}

func TestWSAffinityHotCacheDropsExpiredEntries(t *testing.T) {
	cache := newWSAffinityHotCache(2)
	cache.Set("expired", wsAffinityEntry{ChannelID: 1, ExpiresAt: time.Now().Add(-time.Second)})
	if _, ok := cache.Get("expired", time.Now()); ok {
		t.Fatal("expected expired entry to miss")
	}
	if got := cache.Len(); got != 0 {
		t.Fatalf("expected expired entry to be removed, got size %d", got)
	}
}

func TestNormalizeWSAffinityScopeUsesBoundedCacheKey(t *testing.T) {
	cacheKey, _, ok := normalizeWSAffinityScope(wsAffinityScope{
		APIKeyID:     1,
		GroupID:      2,
		RequestModel: string(make([]byte, 1024*1024)),
		ResponseID:   "resp_1",
	})
	if !ok {
		t.Fatal("expected scope to be valid")
	}
	if len(cacheKey) > 160 {
		t.Fatalf("expected bounded cache key, got %d bytes", len(cacheKey))
	}
}
