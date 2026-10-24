package model

import (
	"encoding/json"
	"strings"
)

func (r *InternalLLMRequest) SetOpenAIRawInputItems(raw json.RawMessage) {
	r.RawInputItems = append(json.RawMessage(nil), raw...)
}
func (r *InternalLLMRequest) OpenAIRawInputItems() json.RawMessage {
	if r == nil {
		return nil
	}
	return r.RawInputItems
}
func (r *InternalLLMRequest) OpenAIPreviousResponseID() string {
	if r == nil || r.PreviousResponseID == nil {
		return ""
	}
	return strings.TrimSpace(*r.PreviousResponseID)
}

func (r *InternalLLMRequest) HasOpenAIConversation() bool {
	if r == nil || r.ProviderExtensions == nil || r.ProviderExtensions.OpenAIResponses == nil || r.ProviderExtensions.OpenAIResponses.Request == nil {
		return false
	}
	raw := r.ProviderExtensions.OpenAIResponses.Request.RawFields["conversation"]
	return len(raw) > 0 && string(raw) != "null"
}
