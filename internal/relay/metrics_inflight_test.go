package relay

import (
	"errors"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// StartLog 把记录挂进 op 的在途表后，只有终态更新会摘掉它。这里固化两条曾经漏掉
// 收尾的路径：提前返回（分组无可用渠道）和被 gin.CustomRecovery 吞掉的 panic。
// 任何一条漏掉，在途表都会永久滞留一条记录——既泄漏内存，也让日志页一直挂着
// 一条「处理中」的幽灵记录。
func TestFinalizeIfUnsavedClearsInFlightLog(t *testing.T) {
	ctx := setupRelayTestDB(t)

	before := op.RelayLogInFlightLen()

	m := NewRelayMetrics(0, "test-model", nil, nil)
	m.StartLog()
	if m.LogID == 0 {
		t.Fatalf("StartLog did not allocate a log id")
	}
	if got := op.RelayLogInFlightLen(); got != before+1 {
		t.Fatalf("in-flight length after StartLog: got %d want %d", got, before+1)
	}

	m.FinalizeIfUnsaved(ctx, errors.New("boom"))

	if got := op.RelayLogInFlightLen(); got != before {
		t.Fatalf("in-flight length after FinalizeIfUnsaved: got %d want %d", got, before)
	}
}

// 已经正常完成的请求不应被兜底重复处理。
func TestFinalizeIfUnsavedIsNoopAfterSave(t *testing.T) {
	ctx := setupRelayTestDB(t)

	before := op.RelayLogInFlightLen()

	m := NewRelayMetrics(0, "test-model", nil, nil)
	m.StartLog()
	m.SaveWithChannelStats(ctx, true, nil, nil, false)

	if got := op.RelayLogInFlightLen(); got != before {
		t.Fatalf("in-flight length after Save: got %d want %d", got, before)
	}
	if !m.logSaved {
		t.Fatalf("expected logSaved to be set by SaveWithChannelStats")
	}

	pendingBefore := op.RelayLogPendingLen()
	m.FinalizeIfUnsaved(ctx, errors.New("should not run"))
	if got := op.RelayLogPendingLen(); got != pendingBefore {
		t.Fatalf("FinalizeIfUnsaved enqueued a duplicate log: pending %d want %d", got, pendingBefore)
	}
}

// 没调用过 StartLog 的链路（compact / ws）不进在途表，兜底必须是纯空操作。
func TestFinalizeIfUnsavedIgnoresMetricsWithoutStartLog(t *testing.T) {
	ctx := setupRelayTestDB(t)

	before := op.RelayLogInFlightLen()
	pendingBefore := op.RelayLogPendingLen()

	m := NewRelayMetrics(0, "test-model", nil, nil)
	m.FinalizeIfUnsaved(ctx, errors.New("boom"))

	if got := op.RelayLogInFlightLen(); got != before {
		t.Fatalf("in-flight length changed: got %d want %d", got, before)
	}
	if got := op.RelayLogPendingLen(); got != pendingBefore {
		t.Fatalf("pending length changed: got %d want %d", got, pendingBefore)
	}
}

// panic 时 defer 仍会执行：模拟 handler 在 StartLog 之后崩溃的情形。
func TestFinalizeIfUnsavedRunsOnPanicUnwind(t *testing.T) {
	ctx := setupRelayTestDB(t)

	before := op.RelayLogInFlightLen()

	func() {
		defer func() { _ = recover() }()
		m := NewRelayMetrics(0, "test-model", nil, nil)
		m.StartLog()
		defer m.FinalizeIfUnsaved(ctx, errRelayAborted)
		panic("simulated handler panic")
	}()

	if got := op.RelayLogInFlightLen(); got != before {
		t.Fatalf("in-flight length after panic unwind: got %d want %d", got, before)
	}
}

func TestRelayMetricsSaveAddsProtocolToLogAndAttempts(t *testing.T) {
	ctx := setupRelayTestDB(t)
	channel := model.Channel{Name: "opencode2api", Type: outbound.OutboundTypeOpenAIResponse, Enabled: true}
	if err := op.ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel failed: %v", err)
	}

	metrics := NewRelayMetrics(0, "test-model", nil, nil)
	metrics.SaveWithChannelStats(ctx, true, nil, []model.ChannelAttempt{{
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		ModelName:   "test-model",
		AttemptNum:  1,
		Status:      model.AttemptSuccess,
	}}, false)

	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("list relay logs failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected one relay log")
	}
	if logs[0].Protocol != "Response" || len(logs[0].Attempts) != 1 || logs[0].Attempts[0].Protocol != "Response" {
		t.Fatalf("expected Response protocol on log and attempt, got %#v", logs[0])
	}
}
