package relay

import (
	"context"
	"fmt"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestIsClientCancellationMatchesWrappedRequestErrors(t *testing.T) {
	ctx := context.Background()

	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.Canceled)) {
		t.Fatalf("expected wrapped context.Canceled to be treated as client cancellation")
	}
	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.DeadlineExceeded)) {
		t.Fatalf("expected wrapped context.DeadlineExceeded to be treated as client cancellation")
	}
}

func TestIsClientCancellationFallsBackToContextState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if !isClientCancellation(ctx, fmt.Errorf("upstream request aborted")) {
		t.Fatalf("expected canceled request context to be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresOrdinaryErrors(t *testing.T) {
	if isClientCancellation(context.Background(), fmt.Errorf("dial tcp timeout")) {
		t.Fatalf("expected ordinary upstream error to not be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresLocalRelayBudgetTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 0, errLocalRelayBudgetExceeded)
	defer cancel()

	<-ctx.Done()
	if isClientCancellation(ctx, contextError(ctx)) {
		t.Fatalf("expected local relay budget timeout to not be treated as client cancellation")
	}
}

func TestStreamResponseCompleted(t *testing.T) {
	stop := "stop"
	empty := ""
	content := "hello world"

	cases := []struct {
		name string
		resp *transformerModel.InternalLLMResponse
		want bool
	}{
		{name: "nil", resp: nil, want: false},
		{name: "no choices", resp: &transformerModel.InternalLLMResponse{}, want: false},
		{
			name: "missing finish reason",
			resp: &transformerModel.InternalLLMResponse{Choices: []transformerModel.Choice{{Index: 0}}},
			want: false,
		},
		{
			name: "empty finish reason",
			resp: &transformerModel.InternalLLMResponse{Choices: []transformerModel.Choice{{Index: 0, FinishReason: &empty}}},
			want: false,
		},
		{
			name: "strict completion",
			resp: &transformerModel.InternalLLMResponse{Choices: []transformerModel.Choice{{Index: 0, FinishReason: &stop}}},
			want: true,
		},
		{
			name: "soft completion with content and usage",
			resp: &transformerModel.InternalLLMResponse{
				Choices: []transformerModel.Choice{{
					Index: 0,
					Message: &transformerModel.Message{
						Content: transformerModel.MessageContent{Content: &content},
					},
				}},
				Usage: &transformerModel.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
			want: true,
		},
		{
			name: "content without usage remains incomplete",
			resp: &transformerModel.InternalLLMResponse{
				Choices: []transformerModel.Choice{{
					Index: 0,
					Message: &transformerModel.Message{
						Content: transformerModel.MessageContent{Content: &content},
					},
				}},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := streamResponseCompleted(tc.resp); got != tc.want {
				t.Fatalf("streamResponseCompleted() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMetricsSuggestCompletedStream(t *testing.T) {
	if !metricsSuggestCompletedStream(&RelayMetrics{Stats: dbmodel.StatsMetrics{InputToken: 10, OutputToken: 5}}) {
		t.Fatalf("expected input and output metrics to suggest completion")
	}
	if metricsSuggestCompletedStream(&RelayMetrics{Stats: dbmodel.StatsMetrics{InputToken: 10}}) {
		t.Fatalf("input-only metrics must not suggest completion")
	}
}

func TestIsFirstTokenTimeoutPreferredOverBareCancel(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errFirstTokenTimeout)
	<-ctx.Done()

	if isClientCancellation(ctx, context.Canceled) {
		t.Fatalf("first-token timeout must not be classified as client cancellation")
	}
	if !isFirstTokenTimeout(ctx, context.Canceled) {
		t.Fatalf("expected first-token timeout detection through context cause")
	}
}

func TestIsRequestTimeoutNotClientCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errRequestTimeout)
	<-ctx.Done()

	if isClientCancellation(ctx, context.Canceled) {
		t.Fatalf("request timeout must not be classified as client cancellation")
	}
	if !isRequestTimeout(ctx, context.Canceled) {
		t.Fatalf("expected request-timeout detection through context cause")
	}
}
