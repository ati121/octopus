package model

import "testing"

func TestMergeToolCallDeltaDoesNotDuplicateFunctionName(t *testing.T) {
	toolCalls := []ToolCall{{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: FunctionCall{
			Name: "Write",
		},
	}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `{"file_path":`,
		},
	})
	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `"a.txt"}`,
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Write" {
		t.Fatalf("function name duplicated: %q", toolCalls[0].Function.Name)
	}
	if toolCalls[0].Function.Arguments != `{"file_path":"a.txt"}` {
		t.Fatalf("arguments not merged: %q", toolCalls[0].Function.Arguments)
	}
}

func TestMergeToolCallDeltaSetsFunctionNameWhenMissing(t *testing.T) {
	toolCalls := []ToolCall{{Index: 0, Type: "function"}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name: "Search",
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Search" {
		t.Fatalf("function name not set: %q", toolCalls[0].Function.Name)
	}
}

func TestStreamAggregatorPreservesResponseError(t *testing.T) {
	aggregator := &StreamAggregator{}
	want := &ResponseError{
		StatusCode: 529,
		Detail: ErrorDetail{
			Type:    "overloaded_error",
			Message: "upstream overloaded",
		},
	}
	aggregator.Add(&InternalLLMResponse{
		ID:     "msg_error",
		Model:  "claude-opus-4-7",
		Object: "chat.completion.chunk",
		Error:  want,
	})

	got := aggregator.BuildAndReset()
	if got == nil || got.Error == nil {
		t.Fatalf("expected aggregated response error, got %+v", got)
	}
	if got.Error.StatusCode != want.StatusCode || got.Error.Detail != want.Detail {
		t.Fatalf("aggregated error = %+v, want %+v", got.Error, want)
	}
}
