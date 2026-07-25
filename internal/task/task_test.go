package task

import (
	"testing"
	"time"
)

func TestUpdateKeepsLatestPendingInterval(t *testing.T) {
	const name = "test_latest_interval"
	entry := &taskEntry{
		name:     name,
		interval: time.Hour,
		fn:       func() {},
		stopCh:   make(chan struct{}),
		updateCh: make(chan time.Duration, 1),
	}
	tasksMu.Lock()
	tasks[name] = entry
	tasksMu.Unlock()
	t.Cleanup(func() {
		tasksMu.Lock()
		delete(tasks, name)
		tasksMu.Unlock()
	})

	Update(name, 2*time.Hour)
	Update(name, 3*time.Hour)
	if got := <-entry.updateCh; got != 3*time.Hour {
		t.Fatalf("expected latest interval, got %v", got)
	}
}

func TestRegisterStartsTaskAfterRunnerStarted(t *testing.T) {
	const name = "test_dynamic_register"
	fired := make(chan struct{}, 1)
	runStarted.Store(true)
	Register(name, 5*time.Millisecond, false, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() {
		Update(name, 0)
		runStarted.Store(false)
	})

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("dynamically registered task did not start")
	}
}
