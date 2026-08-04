package compat

import (
	"testing"
	"time"
)

// 过期条目取不到，且顺手把两个键都清掉。
func TestReplayCacheExpires(t *testing.T) {
	c := newReplayCache(-time.Second, 100)
	c.set("call_expired_0", "tool", "value")
	if n := c.entries.Len(); n != 2 {
		t.Fatalf("entries after set: %d, want 2", n)
	}

	if got := c.get("call_expired_0", "tool"); got != "" {
		t.Fatalf("expired entry returned %q", got)
	}
	// get 会遍历「带 tool name」和兜底两个键，过期的都就地删掉。
	if n := c.entries.Len(); n != 0 {
		t.Fatalf("entries after expired get: %d, want 0", n)
	}
}

// 超量时先清过期，再按到期时间从早到晚淘汰。
func TestReplayCachePruneEvictsOldestFirst(t *testing.T) {
	c := newReplayCache(time.Hour, 2)
	now := time.Now()

	c.entries.Set("old", replayEntry{value: "old", expiresAt: now.Add(time.Minute)})
	c.entries.Set("mid", replayEntry{value: "mid", expiresAt: now.Add(10 * time.Minute)})
	c.entries.Set("new", replayEntry{value: "new", expiresAt: now.Add(time.Hour)})
	c.entries.Set("dead", replayEntry{value: "dead", expiresAt: now.Add(-time.Minute)})

	c.pruneLocked(now)

	if _, ok := c.entries.Get("dead"); ok {
		t.Fatal("expired entry survived prune")
	}
	if _, ok := c.entries.Get("old"); ok {
		t.Fatal("oldest entry survived prune")
	}
	for _, key := range []string{"mid", "new"} {
		if _, ok := c.entries.Get(key); !ok {
			t.Fatalf("entry %q evicted unexpectedly", key)
		}
	}
}

// 空 ID 或空值不写入，避免 "\x00tool" 这类垃圾键。
func TestReplayCacheIgnoresEmptyInput(t *testing.T) {
	c := newReplayCache(time.Hour, 100)
	c.set("", "tool", "value")
	c.set("call_x", "tool", "")
	c.set("  ", "tool", "value")

	if n := c.entries.Len(); n != 0 {
		t.Fatalf("entries: %d, want 0", n)
	}
	if got := c.get("", "tool"); got != "" {
		t.Fatalf("got %q", got)
	}
}
