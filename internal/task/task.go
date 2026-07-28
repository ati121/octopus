package task

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

type taskEntry struct {
	name        string
	interval    time.Duration
	fn          func()
	runOnStart  bool
	ticker      *time.Ticker
	stopCh      chan struct{}
	updateCh    chan time.Duration
	updateMu    sync.Mutex
	running     atomic.Bool
	loopStarted atomic.Bool
}

var (
	tasks      = make(map[string]*taskEntry)
	tasksMu    sync.RWMutex
	runStarted atomic.Bool
)

// Register 注册一个定时任务
// runOnStart: 是否在启动时立即执行一次
func Register(name string, interval time.Duration, runOnStart bool, fn func()) {
	if interval <= 0 {
		log.Debugf("task %s not registered: interval is 0", name)
		return
	}

	tasksMu.Lock()

	if _, exists := tasks[name]; exists {
		tasksMu.Unlock()
		log.Warnf("task %s already registered, skipping", name)
		return
	}

	entry := &taskEntry{
		name:       name,
		interval:   interval,
		fn:         fn,
		runOnStart: runOnStart,
		stopCh:     make(chan struct{}),
		updateCh:   make(chan time.Duration, 1),
	}
	tasks[name] = entry
	tasksMu.Unlock()
	log.Debugf("task %s registered with interval %v, runOnStart: %v", name, interval, runOnStart)
	if runStarted.Load() {
		startTaskLoop(entry)
	}
}

// Update 更新任务的执行间隔
// 当 interval 为 0 时，删除任务
func Update(name string, interval time.Duration) {
	tasksMu.Lock()
	entry, exists := tasks[name]
	if !exists {
		tasksMu.Unlock()
		log.Warnf("task %s not found", name)
		return
	}

	if interval <= 0 {
		delete(tasks, name)
		tasksMu.Unlock()
		close(entry.stopCh)
		log.Infof("task %s removed: interval is 0", name)
		return
	}
	tasksMu.Unlock()

	// Keep only the newest pending interval. A buffered channel avoids losing an
	// update merely because the task loop is handling a ticker at that instant.
	entry.updateMu.Lock()
	select {
	case entry.updateCh <- interval:
	default:
		select {
		case <-entry.updateCh:
		default:
		}
		entry.updateCh <- interval
	}
	entry.updateMu.Unlock()
	log.Infof("task %s interval updated to %v", name, interval)
}

// RUN 启动所有注册的任务
func RUN() {
	runStarted.Store(true)
	tasksMu.RLock()
	entries := make([]*taskEntry, 0, len(tasks))
	for _, entry := range tasks {
		entries = append(entries, entry)
	}
	tasksMu.RUnlock()
	for _, entry := range entries {
		startTaskLoop(entry)
	}

	// 阻塞主协程
	select {}
}

func startTaskLoop(entry *taskEntry) {
	if entry == nil || !entry.loopStarted.CompareAndSwap(false, true) {
		return
	}
	safe.Go("task-loop:"+entry.name, func() {
		runTask(entry)
	})
}

func runTask(entry *taskEntry) {
	// 根据配置决定是否在启动时立即执行
	if entry.runOnStart {
		triggerTask(entry, "startup")
	}

	entry.ticker = time.NewTicker(entry.interval)
	defer entry.ticker.Stop()

	for {
		select {
		case <-entry.ticker.C:
			triggerTask(entry, "ticker")
		case newInterval := <-entry.updateCh:
			entry.ticker.Stop()
			entry.interval = newInterval
			entry.ticker = time.NewTicker(newInterval)
		case <-entry.stopCh:
			return
		}
	}
}

func triggerTask(entry *taskEntry, trigger string) {
	if entry == nil {
		return
	}
	if !entry.running.CompareAndSwap(false, true) {
		log.Warnf("task %s skipped: previous run still in progress (trigger=%s)", entry.name, trigger)
		return
	}
	safe.Go("task-exec:"+entry.name+":"+trigger, func() {
		defer entry.running.Store(false)
		entry.fn()
	})
}
