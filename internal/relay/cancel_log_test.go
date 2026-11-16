package relay

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bestruirui/octopus/internal/op"
)

// 验证两条日志收尾路径实际写出的终态，避免取消被归为上游错误或本地超时。
func TestRelayLogClientCancellationOutcome(t *testing.T) {
	dbctx := setupRelayTestDB(t)
	canceledCtx, cancel := context.WithCancel(dbctx)
	cancel()
	cases := []struct {
		name    string
		ctx     context.Context
		success bool
		err     error
		want499 bool
	}{
		{"wrapped cancellation", dbctx, false, fmt.Errorf("client canceled after partial stream: %w", context.Canceled), true},
		{"canceled context", canceledCtx, false, errors.New("write: broken pipe"), true},
		{"client deadline", dbctx, false, fmt.Errorf("request: %w", context.DeadlineExceeded), true},
		{"upstream failure", dbctx, false, errors.New("upstream returned 503"), false},
		{"completed before disconnect", canceledCtx, true, nil, false},
	}
	for _, timeoutErr := range []error{errLocalRelayBudgetExceeded, errFirstTokenTimeout, errRequestTimeout} {
		timeoutCtx, timeoutCancel := context.WithCancelCause(dbctx)
		timeoutCancel(timeoutErr)
		cases = append(cases, struct {
			name    string
			ctx     context.Context
			success bool
			err     error
			want499 bool
		}{timeoutErr.Error(), timeoutCtx, false, fmt.Errorf("request: %w", context.Canceled), false})
	}
	for _, images := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("images=%t/%s", images, tc.name), func(t *testing.T) {
				var logID int64
				if images {
					m := newImagesRelayMetrics(0, "cancel-test")
					m.StartLog()
					logID = m.LogID
					m.SaveWithChannelStats(tc.ctx, tc.success, tc.err, nil, false)
				} else {
					m := NewRelayMetrics(0, "cancel-test", nil, nil)
					m.StartLog()
					logID = m.LogID
					m.SaveWithChannelStats(tc.ctx, tc.success, tc.err, nil, false)
				}
				entry, err := op.RelayLogGet(dbctx, logID)
				if err != nil {
					t.Fatal(err)
				}
				if entry.Processing || entry.Success != tc.success {
					t.Fatalf("wrong terminal state: %+v", entry)
				}
				if tc.want499 {
					if entry.StatusCode != 499 || entry.Error != "客户端已取消" {
						t.Fatalf("cancellation must have explicit 499 and a concise message: %+v", entry)
					}
				} else {
					if entry.StatusCode == 499 {
						t.Fatalf("completion or local/upstream failure must not become cancellation: %+v", entry)
					}
					wantError := ""
					if tc.err != nil {
						wantError = tc.err.Error()
					}
					if entry.Error != wantError {
						t.Fatalf("original error lost: got %q want %q", entry.Error, wantError)
					}
				}
			})
		}
	}
}
