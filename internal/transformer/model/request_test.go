package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func requestTestStringPtr(value string) *string {
	return &value
}

func TestInternalLLMRequestValidateFillsStableToolCallIDs(t *testing.T) {
	makeRequest := func(prefix bool) *InternalLLMRequest {
		messages := []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"octopus","limit":1}`,
				},
			}},
		}}
		if prefix {
			messages = append([]Message{{Role: "user", Content: MessageContent{Content: requestTestStringPtr("prefix")}}}, messages...)
		}
		return &InternalLLMRequest{Model: "gpt-4o", Messages: messages}
	}

	first := makeRequest(false)
	second := makeRequest(true)
	if err := first.Validate(); err != nil {
		t.Fatalf("validate first: %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("validate second: %v", err)
	}
	firstID := first.Messages[0].ToolCalls[0].ID
	secondID := second.Messages[1].ToolCalls[0].ID
	if firstID == "" || secondID == "" {
		t.Fatalf("expected generated IDs, got %q and %q", firstID, secondID)
	}
	if firstID != secondID {
		t.Fatalf("expected ID independent of message index, got %q and %q", firstID, secondID)
	}
}

func TestInternalLLMRequestValidatePreservesExistingToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_existing",
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{}`,
				},
			}},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := req.Messages[0].ToolCalls[0].ID; got != "call_existing" {
		t.Fatalf("expected existing ID preserved, got %q", got)
	}
}

func TestInternalLLMRequestValidateDisambiguatesDuplicateGeneratedToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
			},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	first := req.Messages[0].ToolCalls[0].ID
	second := req.Messages[0].ToolCalls[1].ID
	if first == "" || second == "" || first == second {
		t.Fatalf("expected unique generated IDs, got %q and %q", first, second)
	}
}

func TestInternalLLMRequestValidateAllowsResponsesRawInputItems(t *testing.T) {
	req := &InternalLLMRequest{
		Model:        "gpt-4o",
		RawAPIFormat: APIFormatOpenAIResponse,
		RawInputItems: json.RawMessage(`[
			{"type":"computer_call","id":"call_1"}
		]`),
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected raw responses input items to satisfy validation, got %v", err)
	}
	if !req.IsChatRequest() {
		t.Fatalf("expected raw responses input items to be treated as chat request")
	}
}

func TestInternalLLMRequestValidateRejectsRawInputItemsWithoutResponsesFormat(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		RawInputItems: json.RawMessage(`[
			{"type":"input_text","text":"hello"}
		]`),
	}

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "raw_input_items require OpenAI Responses api format") {
		t.Fatalf("expected raw_input_items format validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsInvalidRawInputItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{
			name: "object",
			raw:  json.RawMessage(`{"type":"input_text","text":"hello"}`),
		},
		{
			name: "null",
			raw:  json.RawMessage(`null`),
		},
		{
			name: "empty array",
			raw:  json.RawMessage(`[]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InternalLLMRequest{
				Model:         "gpt-4o",
				RawAPIFormat:  APIFormatOpenAIResponse,
				RawInputItems: tt.raw,
			}

			if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "raw_input_items must be a valid JSON array") {
				t.Fatalf("expected raw_input_items json array validation error, got %v", err)
			}
		})
	}
}

func TestInternalLLMRequestValidateRejectsPreviousResponseIDWithoutResponsesFormat(t *testing.T) {
	previousResponseID := "resp_prev"
	req := &InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: &previousResponseID,
		Messages:           []Message{{Role: "user", Content: MessageContent{Content: requestTestStringPtr("hello")}}},
	}

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "previous_response_id requires OpenAI Responses api format") {
		t.Fatalf("expected previous_response_id format validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsReplayExactWithPreviousResponseID(t *testing.T) {
	previousResponseID := "resp_prev"
	req := &InternalLLMRequest{
		Model:              "gpt-4o",
		RawAPIFormat:       APIFormatOpenAIResponse,
		PreviousResponseID: &previousResponseID,
		RawInputItems:      json.RawMessage(`[{"type":"input_text","text":"hello"}]`),
	}
	req.MarkOpenAIExactReplayRequest()

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "replay_exact request must not include previous_response_id") {
		t.Fatalf("expected replay_exact previous_response_id validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsReplayExactWithoutRawInputItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "missing"},
		{name: "null", raw: json.RawMessage(`null`)},
		{name: "empty array", raw: json.RawMessage(`[]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InternalLLMRequest{
				Model:         "gpt-4o",
				RawAPIFormat:  APIFormatOpenAIResponse,
				RawInputItems: tt.raw,
			}
			req.MarkOpenAIExactReplayRequest()

			if err := req.Validate(); err == nil {
				t.Fatalf("expected replay_exact raw_input_items validation error")
			}
		})
	}
}

func TestInternalLLMRequestMarkOpenAIExactReplayRequest(t *testing.T) {
	req := &InternalLLMRequest{}
	req.MarkOpenAIExactReplayRequest()

	if !req.IsOpenAIExactReplayRequest() {
		t.Fatalf("expected marked request to be detected as exact replay")
	}
	if req.TransformerMetadata[TransformerMetadataWSExecutionMode] != TransformerMetadataWSExecutionModeReplayExact {
		t.Fatalf("unexpected ws execution mode metadata: %#v", req.TransformerMetadata)
	}
}

func TestInternalLLMRequestOpenAIResponsesOptionsRoundTripAndClone(t *testing.T) {
	previousID := "resp_prev"
	background := true
	cacheKey := "cache-key"
	retention := "24h"
	safetyID := "safe-user"
	maxToolCalls := int64(8)
	summary := "auto"
	generateSummary := "concise"
	req := &InternalLLMRequest{}

	req.SetOpenAIResponsesOptions(OpenAIResponsesOptions{
		PreviousResponseID:       &previousID,
		Background:               &background,
		Prompt:                   json.RawMessage(`{"id":"prompt_1"}`),
		PromptCacheKey:           &cacheKey,
		PromptCacheRetention:     &retention,
		SafetyIdentifier:         &safetyID,
		MaxToolCalls:             &maxToolCalls,
		Conversation:             json.RawMessage(`{"id":"conv_1"}`),
		ContextManagement:        json.RawMessage(`{"strategy":"auto"}`),
		StreamOptions:            json.RawMessage(`{"include_usage":true}`),
		ReasoningSummary:         &summary,
		ReasoningGenerateSummary: &generateSummary,
		RawInputItems:            json.RawMessage(`[{"type":"input_text","text":"hello"}]`),
	})

	options := req.GetOpenAIResponsesOptions()
	if options.PreviousResponseID == nil || *options.PreviousResponseID != previousID {
		t.Fatalf("expected previous response id to round-trip, got %#v", options.PreviousResponseID)
	}
	if !strings.Contains(string(options.Prompt), "prompt_1") || !strings.Contains(string(options.RawInputItems), "hello") {
		t.Fatalf("expected raw responses options to round-trip, got prompt=%s raw=%s", options.Prompt, options.RawInputItems)
	}
	*options.PreviousResponseID = "mutated"
	options.RawInputItems[0] = 'x'
	if req.OpenAIPreviousResponseID() != previousID {
		t.Fatalf("expected previous response id getter to be clone-safe, got %q", req.OpenAIPreviousResponseID())
	}
	if !strings.Contains(string(req.RawInputItems), "hello") {
		t.Fatalf("expected raw input items to be clone-safe, got %s", req.RawInputItems)
	}
}
