package model

import (
	"encoding/json"
	"strings"
)

type OpenAIResponsesOptions struct {
	PreviousResponseID       *string         `json:"previous_response_id,omitempty"`
	Background               *bool           `json:"background,omitempty"`
	Prompt                   json.RawMessage `json:"prompt,omitempty"`
	PromptCacheKey           *string         `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention     *string         `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier         *string         `json:"safety_identifier,omitempty"`
	MaxToolCalls             *int64          `json:"max_tool_calls,omitempty"`
	Conversation             json.RawMessage `json:"conversation,omitempty"`
	ContextManagement        json.RawMessage `json:"context_management,omitempty"`
	StreamOptions            json.RawMessage `json:"stream_options,omitempty"`
	ReasoningSummary         *string         `json:"reasoning_summary,omitempty"`
	ReasoningGenerateSummary *string         `json:"reasoning_generate_summary,omitempty"`
	RawInputItems            json.RawMessage `json:"raw_input_items,omitempty"`
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (r *InternalLLMRequest) SetOpenAIRawInputItems(raw json.RawMessage) {
	if r == nil {
		return
	}
	r.RawInputItems = cloneRawMessage(raw)
}

func (r *InternalLLMRequest) SetOpenAIResponsesOptions(options OpenAIResponsesOptions) {
	if r == nil {
		return
	}
	r.PreviousResponseID = cloneStringPtr(options.PreviousResponseID)
	r.Background = cloneBoolPtr(options.Background)
	r.Prompt = cloneRawMessage(options.Prompt)
	r.ResponsesPromptCacheKey = cloneStringPtr(options.PromptCacheKey)
	r.PromptCacheRetention = cloneStringPtr(options.PromptCacheRetention)
	r.SafetyIdentifier = cloneStringPtr(options.SafetyIdentifier)
	r.MaxToolCalls = cloneInt64Ptr(options.MaxToolCalls)
	r.Conversation = cloneRawMessage(options.Conversation)
	r.ContextManagement = cloneRawMessage(options.ContextManagement)
	r.ResponsesStreamOptions = cloneRawMessage(options.StreamOptions)
	r.ReasoningSummary = cloneStringPtr(options.ReasoningSummary)
	r.ReasoningGenerateSummary = cloneStringPtr(options.ReasoningGenerateSummary)
	r.SetOpenAIRawInputItems(options.RawInputItems)
}

func (r *InternalLLMRequest) GetOpenAIResponsesOptions() OpenAIResponsesOptions {
	if r == nil {
		return OpenAIResponsesOptions{}
	}
	return OpenAIResponsesOptions{
		PreviousResponseID:       cloneStringPtr(r.PreviousResponseID),
		Background:               cloneBoolPtr(r.Background),
		Prompt:                   cloneRawMessage(r.Prompt),
		PromptCacheKey:           cloneStringPtr(r.ResponsesPromptCacheKey),
		PromptCacheRetention:     cloneStringPtr(r.PromptCacheRetention),
		SafetyIdentifier:         cloneStringPtr(r.SafetyIdentifier),
		MaxToolCalls:             cloneInt64Ptr(r.MaxToolCalls),
		Conversation:             cloneRawMessage(r.Conversation),
		ContextManagement:        cloneRawMessage(r.ContextManagement),
		StreamOptions:            cloneRawMessage(r.ResponsesStreamOptions),
		ReasoningSummary:         cloneStringPtr(r.ReasoningSummary),
		ReasoningGenerateSummary: cloneStringPtr(r.ReasoningGenerateSummary),
		RawInputItems:            cloneRawMessage(r.RawInputItems),
	}
}

func (r *InternalLLMRequest) OpenAIRawInputItems() json.RawMessage {
	if r == nil {
		return nil
	}
	return cloneRawMessage(r.RawInputItems)
}

func (r *InternalLLMRequest) OpenAIPreviousResponseID() string {
	if r == nil || r.PreviousResponseID == nil {
		return ""
	}
	return strings.TrimSpace(*r.PreviousResponseID)
}
