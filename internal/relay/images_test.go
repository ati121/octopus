package relay

import (
	"context"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestImagesRelayMetricsLogLifecycle(t *testing.T) {
	events := op.RelayLogSubscribe()
	defer op.RelayLogUnsubscribe(events)

	metrics := newImagesRelayMetrics(0, "gpt-image-2")
	metrics.RequestContent = `{"model":"gpt-image-2"}`
	metrics.StartLog()

	started := receiveRelayLogEvent(t, events)
	if metrics.LogID == 0 || started.ID != metrics.LogID {
		t.Fatalf("expected started log ID %d, got %d", metrics.LogID, started.ID)
	}
	if !started.Processing || started.RequestModelName != "gpt-image-2" {
		t.Fatalf("expected processing image log, got %+v", started)
	}

	metrics.ActualModel = "gpt-image-2-1k"
	metrics.ResponseContent = `{"stream":false}`
	metrics.saveLog(context.Background(), true, nil, time.Second, []model.ChannelAttempt{{
		ChannelID:   121,
		ChannelName: "image-channel",
		ModelName:   "gpt-image-2-1k",
		AttemptNum:  1,
		Status:      model.AttemptSuccess,
	}}, 121, "image-channel")

	completed := receiveRelayLogEvent(t, events)
	if completed.ID != started.ID {
		t.Fatalf("expected completed log to reuse ID %d, got %d", started.ID, completed.ID)
	}
	if completed.Processing || !completed.Success {
		t.Fatalf("expected successful completed image log, got %+v", completed)
	}
	if completed.ChannelId != 121 || completed.ActualModelName != "gpt-image-2-1k" {
		t.Fatalf("unexpected completed image log metadata: %+v", completed)
	}
}

func receiveRelayLogEvent(t *testing.T, events <-chan model.RelayLog) model.RelayLog {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for relay log event")
		return model.RelayLog{}
	}
}
