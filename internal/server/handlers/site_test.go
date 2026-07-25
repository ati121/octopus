package handlers

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSiteProjectionJobsBoundsConcurrency(t *testing.T) {
	ids := make([]int, 20)
	for i := range ids {
		ids[i] = i + 1
	}
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32
	runSiteProjectionJobs(ids, 3, func(int) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		completed.Add(1)
	})
	if got := maximum.Load(); got > 3 {
		t.Fatalf("projection concurrency exceeded limit: %d", got)
	}
	if got := completed.Load(); got != int32(len(ids)) {
		t.Fatalf("expected %d projections, got %d", len(ids), got)
	}
}
