package balancer

import (
	"fmt"
	"testing"
	"time"
)

func TestStickySessionStoreIsBounded(t *testing.T) {
	resetStickySessions()
	t.Cleanup(resetStickySessions)
	for i := range maxStickySessionEntries + 20 {
		SetSticky(i, fmt.Sprintf("model-%d", i), i+1, i+1)
	}
	globalSession.mu.Lock()
	count := len(globalSession.entries)
	globalSession.mu.Unlock()
	if count > maxStickySessionEntries {
		t.Fatalf("sticky session store exceeded limit: %d", count)
	}
}

func TestGetStickyDeletesExpiredEntry(t *testing.T) {
	resetStickySessions()
	t.Cleanup(resetStickySessions)
	SetSticky(1, "model", 2, 3)
	if got := GetSticky(1, "model", -time.Second); got != nil {
		t.Fatalf("expected expired sticky entry, got %#v", got)
	}
}
