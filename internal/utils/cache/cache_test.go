package cache

import (
	"sync"
	"testing"
)

func TestUpdateIsAtomic(t *testing.T) {
	c := New[string, int](16)
	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			c.Update("counter", func(current int, _ bool) int {
				return current + 1
			})
		}()
	}
	wg.Wait()

	got, ok := c.Get("counter")
	if !ok || got != workers {
		t.Fatalf("expected %d atomic updates, got %d (exists=%t)", workers, got, ok)
	}
}

func TestUpdateIfPresentDoesNotInsertMissingKey(t *testing.T) {
	c := New[string, int](16)
	if _, ok := c.UpdateIfPresent("missing", func(current int) int { return current + 1 }); ok {
		t.Fatal("expected missing key update to be rejected")
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing key should not have been inserted")
	}

	c.Set("existing", 1)
	updated, ok := c.UpdateIfPresent("existing", func(current int) int { return current + 1 })
	if !ok || updated != 2 {
		t.Fatalf("expected existing key to update to 2, got %d (ok=%t)", updated, ok)
	}
}
