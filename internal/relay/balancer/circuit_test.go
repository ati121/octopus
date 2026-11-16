package balancer

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestResetCircuitBreakerByChannelRemovesOnlyTargetChannel(t *testing.T) {
	enableCircuitBreakerForTest(t)
	Reset()
	setCircuitEntryStateForTest(t, circuitKey(1, 10, "gpt-4o"), StateOpen, time.Now(), 1)
	setCircuitEntryStateForTest(t, circuitKey(10, 10, "gpt-4o"), StateOpen, time.Now(), 1)
	setCircuitEntryStateForTest(t, circuitKey(2, 20, "gpt-4o"), StateOpen, time.Now(), 1)

	ResetStateByChannel(1)

	if tripped, _ := IsTripped(1, 10, "gpt-4o"); tripped {
		t.Fatal("expected target channel circuit breaker to be reset")
	}
	if tripped, _ := IsTripped(10, 10, "gpt-4o"); !tripped {
		t.Fatal("expected channel with similar prefix to remain tripped")
	}
	if tripped, _ := IsTripped(2, 20, "gpt-4o"); !tripped {
		t.Fatal("expected unrelated channel circuit breaker to remain tripped")
	}
}

func TestPruneCircuitBreakersRemovesIdleClosedEntry(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	key := circuitKey(99, 100, "idle-model")
	entry := &circuitEntry{
		State:            StateClosed,
		LastActivityTime: time.Now().Add(-2 * circuitBreakerIdleTTL),
	}
	circuitBreakerStoreMu.Lock()
	globalBreaker.Store(key, entry)
	circuitBreakerEntryCount++
	circuitBreakerStoreMu.Unlock()
	pruneCircuitBreakers(time.Now())
	if _, ok := globalBreaker.Load(key); ok {
		t.Fatal("idle closed circuit entry was not pruned")
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}

func TestClearCircuitBreakersPreservesStickySessions(t *testing.T) {
	enableCircuitBreakerForTest(t)
	Reset()
	t.Cleanup(Reset)
	setCircuitEntryStateForTest(t, circuitKey(1, 10, "gpt-4o"), StateOpen, time.Now(), 1)
	SetSticky(7, "gpt-4o", 10, 100)

	ClearCircuitBreakers()

	if snapshots := ListCircuitSnapshots(); len(snapshots) != 0 {
		t.Fatalf("expected circuit state to be cleared, got %#v", snapshots)
	}
	if entry := GetSticky(7, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 10 {
		t.Fatalf("expected sticky session to be preserved, got %#v", entry)
	}
}

func TestHalfOpenDoesNotRemainTrippedForeverWithoutResult(t *testing.T) {
	enableCircuitBreakerForTest(t)
	Reset()
	key := circuitKey(7, 8, "gpt-4o")
	entry := getOrCreateEntry(key)
	if entry == nil {
		t.Fatal("expected circuit entry")
	}
	entry.mu.Lock()
	entry.State = StateHalfOpen
	entry.TripCount = 1
	entry.HalfOpenSince = time.Now().Add(-61 * time.Second)
	entry.mu.Unlock()

	tripped, remaining := IsTripped(7, 8, "gpt-4o")
	if !tripped {
		t.Fatal("expected expired half-open probe to be tripped again")
	}
	if remaining <= 0 {
		t.Fatalf("expected expired half-open probe to return cooldown, got %v", remaining)
	}

	value, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatal("expected circuit entry to remain after half-open timeout")
	}
	storedEntry := value.(*circuitEntry)
	storedEntry.mu.Lock()
	defer storedEntry.mu.Unlock()
	if storedEntry.State != StateOpen {
		t.Fatalf("expected expired half-open entry to return to open, got %v", storedEntry.State)
	}
	if !storedEntry.HalfOpenSince.IsZero() {
		t.Fatalf("expected half-open timestamp to be cleared, got %v", storedEntry.HalfOpenSince)
	}
}

func TestCircuitBreakerRejectsNewEntriesAtCapacity(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	for i := 0; i < maxCircuitBreakerEntries; i++ {
		if entry := getOrCreateEntry(circuitKey(1, i+1, "model")); entry == nil {
			t.Fatalf("entry %d was rejected before reaching capacity", i)
		}
	}
	if entry := getOrCreateEntry(circuitKey(2, 1, "overflow")); entry != nil {
		t.Fatal("expected entry above capacity to be rejected")
	}
	if got := circuitBreakerCount(); got != maxCircuitBreakerEntries {
		t.Fatalf("expected %d entries, got %d", maxCircuitBreakerEntries, got)
	}
}

func TestSoftFailureDoesNotCreateCircuitEntry(t *testing.T) {
	enableCircuitBreakerForTest(t)
	Reset()
	RecordFailure(1, 1, "rate-limited-model", FailureSoftRateLimit)
	if got := circuitBreakerCount(); got != 0 {
		t.Fatalf("expected no entry for a new soft failure, got %d", got)
	}
}

func TestCircuitBreakerSettingDisablesAndResetsState(t *testing.T) {
	if db.GetDB() != nil {
		_ = db.Close()
	}
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "circuit-settings.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		configureCircuitBreaker(false)
		Reset()
		_ = db.Close()
	})
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	value, err := op.SettingGetString(model.SettingKeyCircuitBreakerEnabled)
	if err != nil || value != "false" {
		t.Fatalf("expected missing setting to default to off, value=%q err=%v", value, err)
	}
	for i := 0; i < 10; i++ {
		RecordFailure(1, 2, "model", FailureHard)
	}
	if circuitBreakerCount() != 0 {
		t.Fatal("disabled circuit breaker accumulated failures")
	}
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerThreshold, 2); err != nil {
		t.Fatal(err)
	}
	setEnabled := func(value string) {
		t.Helper()
		if err := op.SettingSetString(model.SettingKeyCircuitBreakerEnabled, value); err != nil {
			t.Fatal(err)
		}
	}
	setEnabled("true")
	RecordFailure(1, 2, "model", FailureHard)
	RecordFailure(1, 2, "model", FailureHard)
	if tripped, _ := IsTripped(1, 2, "model"); !tripped {
		t.Fatal("enabled circuit did not trip at its threshold")
	}
	SetSticky(7, "model", 1, 2)
	setEnabled("false")
	if circuitBreakerCount() != 0 {
		t.Fatal("turning off did not immediately clear existing entries")
	}
	if tripped, remaining := IsTripped(1, 2, "model"); tripped || remaining != 0 {
		t.Fatal("disabled circuit still blocks forwarding")
	}
	if sticky := GetSticky(7, "model", time.Minute); sticky == nil {
		t.Fatal("turning off cleared session affinity")
	}
	setEnabled("true")
	RecordFailure(1, 2, "model", FailureHard)
	if tripped, _ := IsTripped(1, 2, "model"); tripped {
		t.Fatal("reenabling reused previous failures")
	}
	// 模拟导入备份或重载配置：数据库中的关闭状态也必须同步到转发路径。
	if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", model.SettingKeyCircuitBreakerEnabled).Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	RecordFailure(1, 2, "model", FailureHard)
	if circuitBreakerCount() != 0 {
		t.Fatal("configuration reload left circuit breaker active")
	}
}

func TestDisablingCircuitBreakerWhileFailuresFinish(t *testing.T) {
	enableCircuitBreakerForTest(t)
	var wg sync.WaitGroup
	for keyID := 1; keyID <= 8; keyID++ {
		wg.Go(func() {
			for i := 0; i < 100; i++ {
				RecordFailure(1, keyID, "model", FailureHard)
				IsTripped(1, keyID, "model")
			}
		})
	}
	configureCircuitBreaker(false)
	wg.Wait()
	if circuitBreakerCount() != 0 || len(ListCircuitSnapshots()) != 0 {
		t.Fatal("in-flight failures recreated circuit state after disabling")
	}
}

func enableCircuitBreakerForTest(t *testing.T) {
	t.Helper()
	configureCircuitBreaker(true)
	t.Cleanup(func() { configureCircuitBreaker(false) })
}

func setCircuitEntryStateForTest(t *testing.T, key string, state CircuitState, lastFailure time.Time, tripCount int) {
	t.Helper()
	entry := getOrCreateEntry(key)
	if entry == nil {
		t.Fatalf("failed to create circuit entry %q", key)
	}
	entry.mu.Lock()
	entry.State = state
	entry.LastFailureTime = lastFailure
	entry.LastActivityTime = lastFailure
	entry.TripCount = tripCount
	entry.mu.Unlock()
}
