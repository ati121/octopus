package relay

import (
	"context"
	"errors"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

var (
	errLocalRelayBudgetExceeded = errors.New("local relay budget exceeded")
	errFirstTokenTimeout        = errors.New("first token timeout")
	errRequestTimeout           = errors.New("request timeout")
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func isLocalRelayBudgetExceeded(ctx context.Context, err error) bool {
	if errors.Is(err, errLocalRelayBudgetExceeded) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errLocalRelayBudgetExceeded)
}

func isFirstTokenTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, errFirstTokenTimeout) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errFirstTokenTimeout)
}

func isRequestTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, errRequestTimeout) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errRequestTimeout)
}

func isClientCancellation(ctx context.Context, err error) bool {
	if isLocalRelayBudgetExceeded(ctx, err) || isLocalRelayBudgetExceeded(ctx, contextError(ctx)) ||
		isFirstTokenTimeout(ctx, err) || isFirstTokenTimeout(ctx, contextError(ctx)) ||
		isRequestTimeout(ctx, err) || isRequestTimeout(ctx, contextError(ctx)) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// streamResponseCompleted reports whether an aggregated stream response is
// complete enough that a trailing client disconnect should count as success.
// Besides strict finish_reason completion, content plus usage is accepted for
// relay stations that drop the final SSE marker after delivering the turn.
func streamResponseCompleted(resp *model.InternalLLMResponse) bool {
	if resp == nil {
		return false
	}
	if len(resp.EmbeddingData) > 0 {
		return true
	}
	if len(resp.Choices) == 0 {
		return false
	}

	allFinished := true
	hasContent := false
	for i := range resp.Choices {
		choice := &resp.Choices[i]
		if choice.FinishReason == nil || strings.TrimSpace(*choice.FinishReason) == "" {
			allFinished = false
		}
		if choiceHasDeliveredContent(choice) {
			hasContent = true
		}
	}
	if allFinished {
		return true
	}
	if hasContent && resp.Usage != nil {
		return resp.Usage.CompletionTokens > 0 || resp.Usage.PromptTokens > 0 || resp.Usage.TotalTokens > 0
	}
	return false
}

func choiceHasDeliveredContent(choice *model.Choice) bool {
	if choice == nil {
		return false
	}
	message := choice.Message
	if message == nil && choice.Delta != nil {
		message = choice.Delta
	}
	if message == nil {
		return false
	}
	if message.Content.Content != nil && strings.TrimSpace(*message.Content.Content) != "" {
		return true
	}
	if len(message.Content.MultipleContent) > 0 || len(message.ToolCalls) > 0 {
		return true
	}
	return message.ReasoningContent != nil && strings.TrimSpace(*message.ReasoningContent) != ""
}

// metricsSuggestCompletedStream covers outer-handler races where the stream
// already populated aggregate metrics before cancellation became visible.
func metricsSuggestCompletedStream(metrics *RelayMetrics) bool {
	if metrics == nil {
		return false
	}
	if streamResponseCompleted(metrics.InternalResponse) {
		return true
	}
	return metrics.Stats.OutputToken > 0 && metrics.Stats.InputToken > 0
}
