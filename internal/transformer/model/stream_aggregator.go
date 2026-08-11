package model

import "sort"

type StreamAggregator struct {
	chunks []*InternalLLMResponse
}

func (a *StreamAggregator) Add(chunk *InternalLLMResponse) {
	if chunk == nil || chunk.Object == "[DONE]" {
		return
	}
	a.chunks = append(a.chunks, chunk)
}

func (a *StreamAggregator) Reset() {
	a.chunks = nil
}

func (a *StreamAggregator) Response() *InternalLLMResponse {
	if a == nil || len(a.chunks) == 0 {
		return nil
	}

	firstChunk := a.chunks[0]
	result := &InternalLLMResponse{
		ID:                firstChunk.ID,
		Object:            "chat.completion",
		Created:           firstChunk.Created,
		Model:             firstChunk.Model,
		SystemFingerprint: firstChunk.SystemFingerprint,
		ServiceTier:       firstChunk.ServiceTier,
	}
	choicesMap := make(map[int]*Choice)

	for _, chunk := range a.chunks {
		if chunk == nil {
			continue
		}
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
		}
		if chunk.Error != nil {
			result.Error = chunk.Error
		}
		for _, choice := range chunk.Choices {
			existingChoice := choicesMap[choice.Index]
			if existingChoice == nil {
				existingChoice = &Choice{Index: choice.Index, Message: &Message{}}
				choicesMap[choice.Index] = existingChoice
			}
			mergeChoiceDelta(existingChoice, choice)
		}
	}

	result.Choices = make([]Choice, 0, len(choicesMap))
	indices := make([]int, 0, len(choicesMap))
	for idx := range choicesMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		result.Choices = append(result.Choices, *choicesMap[idx])
	}
	return result
}

func (a *StreamAggregator) BuildAndReset() *InternalLLMResponse {
	response := a.Response()
	a.Reset()
	return response
}

func mergeChoiceDelta(existingChoice *Choice, choice Choice) {
	if choice.Delta != nil {
		delta := choice.Delta
		if delta.Role != "" {
			existingChoice.Message.Role = delta.Role
		}
		if delta.Content.Content != nil {
			if existingChoice.Message.Content.Content == nil {
				existingChoice.Message.Content.Content = new(string)
			}
			*existingChoice.Message.Content.Content += *delta.Content.Content
		}
		if len(delta.Content.MultipleContent) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Content.MultipleContent...)
		}
		if len(delta.Images) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Images...)
		}
		if delta.Audio != nil {
			if existingChoice.Message.Audio == nil {
				existingChoice.Message.Audio = &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{}
			}
			if delta.Audio.ID != "" {
				existingChoice.Message.Audio.ID = delta.Audio.ID
			}
			if delta.Audio.ExpiresAt > 0 {
				existingChoice.Message.Audio.ExpiresAt = delta.Audio.ExpiresAt
			}
			existingChoice.Message.Audio.Data += delta.Audio.Data
			existingChoice.Message.Audio.Transcript += delta.Audio.Transcript
		}
		if reasoning := delta.GetReasoningContent(); reasoning != "" {
			if existingChoice.Message.ReasoningContent == nil {
				existingChoice.Message.ReasoningContent = new(string)
			}
			*existingChoice.Message.ReasoningContent += reasoning
		}
		for _, toolCall := range delta.ToolCalls {
			existingChoice.Message.ToolCalls = MergeToolCallDelta(existingChoice.Message.ToolCalls, toolCall)
		}
		if delta.Refusal != "" {
			existingChoice.Message.Refusal = delta.Refusal
		}
		// 搜索来源按 URL 去重累积：结果块与逐句 citations_delta 会指向同一批
		// URL，直接 append 会让聚合后的响应出现大量重复来源。
		existingChoice.Message.Annotations = mergeAnnotations(existingChoice.Message.Annotations, delta.Annotations)
		existingChoice.Message.SearchSources = mergeSearchSources(existingChoice.Message.SearchSources, delta.SearchSources)
	}
	if choice.FinishReason != nil {
		existingChoice.FinishReason = choice.FinishReason
	}
	if choice.Logprobs != nil {
		if existingChoice.Logprobs == nil {
			existingChoice.Logprobs = &LogprobsContent{}
		}
		existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, choice.Logprobs.Content...)
	}
}

// mergeAnnotations 累积 annotations，按 URL + cited_text 去重。
// 同一 URL 的不同引用片段是有意义的独立标注，因此不能只按 URL 去重。
func mergeAnnotations(existing, incoming []Annotation) []Annotation {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	key := func(a Annotation) string {
		if a.URLCitation == nil {
			return a.Type
		}
		return a.Type + "\x00" + a.URLCitation.URL + "\x00" + a.URLCitation.CitedText
	}
	for _, a := range existing {
		seen[key(a)] = struct{}{}
	}
	for _, a := range incoming {
		k := key(a)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		existing = append(existing, a)
	}
	return existing
}

// mergeSearchSources 累积来源列表，按 URL 去重。
func mergeSearchSources(existing, incoming []SearchSource) []SearchSource {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, s := range existing {
		seen[s.URL] = struct{}{}
	}
	for _, s := range incoming {
		if _, dup := seen[s.URL]; dup {
			continue
		}
		seen[s.URL] = struct{}{}
		existing = append(existing, s)
	}
	return existing
}

func MergeToolCallDelta(toolCalls []ToolCall, delta ToolCall) []ToolCall {
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			if delta.Function.Name != "" {
				if toolCalls[i].Function.Name == "" {
					toolCalls[i].Function.Name = delta.Function.Name
				} else if toolCalls[i].Function.Name != delta.Function.Name {
					toolCalls[i].Function.Name += delta.Function.Name
				}
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Function.Arguments += delta.Function.Arguments
			}
			return toolCalls
		}
	}
	return append(toolCalls, delta)
}
