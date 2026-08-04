package openai

import (
	"context"
	"encoding/json"

	"github.com/bestruirui/octopus/internal/transformer/compat"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

type ChatInbound struct {
	streamAggregator model.StreamAggregator
	// storedResponse stores the non-stream response
	storedResponse *model.InternalLLMResponse
}

func (i *ChatInbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var request model.InternalLLMRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	// O-H2: tag the origin so outbound transformers (raw passthrough,
	// alternation enforcement, schema conversion) can tell a Chat request
	// apart from a Responses request.
	request.RawAPIFormat = model.APIFormatOpenAIChatCompletion
	// F-1: 捕获 InternalLLMRequest 未建模的顶层字段，供 Chat → Chat 同格式路径
	// 在 outbound 序列化后合并回上游请求体，避免静默丢弃前向兼容参数。
	model.CaptureUnknownRequestFields(&request, body)
	// G-H7: Chat 协议没有 thoughtSignature 字段，客户端回传的历史 tool_calls 里签名
	// 必然为空。按 tool call ID 从网关缓存回填，否则 Gemini 3 会以
	// "Function call is missing a thought_signature" 拒绝整轮重放。
	for idx := range request.Messages {
		compat.RestoreGeminiToolCallSignatures(request.Messages[idx].ToolCalls)
	}
	return &request, nil
}

func (i *ChatInbound) TransformResponse(ctx context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	// Store the response for later retrieval
	i.storedResponse = response
	// 签名不会随 Chat 响应发给客户端，落库到网关缓存，等下一轮回传时补回。
	saveGeminiSignaturesFromResponse(response)

	body, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// saveGeminiSignaturesFromResponse 从一次响应（或一个流式分片）里提取 tool call 上的
// Gemini thoughtSignature 存入缓存。非流式落在 Message 上，流式落在 Delta 上。
func saveGeminiSignaturesFromResponse(response *model.InternalLLMResponse) {
	if response == nil {
		return
	}
	for idx := range response.Choices {
		if msg := response.Choices[idx].Message; msg != nil {
			compat.SaveGeminiToolCallSignatures(msg.ToolCalls)
		}
		if delta := response.Choices[idx].Delta; delta != nil {
			compat.SaveGeminiToolCallSignatures(delta.ToolCalls)
		}
	}
}

func (i *ChatInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	if stream.Object == "[DONE]" {
		return []byte("data: [DONE]\n\n"), nil
	}

	// Store the chunk for aggregation
	i.streamAggregator.Add(stream)
	saveGeminiSignaturesFromResponse(stream)

	var body []byte
	var err error

	// Handle the case where choices are empty but we need them to be present as an empty array
	// This is to satisfy some clients (like Cherry Studio) that require choices field to be present
	if len(stream.Choices) == 0 && stream.Object == "chat.completion.chunk" {
		type Alias model.InternalLLMResponse
		aux := &struct {
			*Alias
			Choices []model.Choice `json:"choices"`
		}{
			Alias:   (*Alias)(stream),
			Choices: []model.Choice{},
		}
		body, err = json.Marshal(aux)
	} else {
		body, err = json.Marshal(stream)
	}

	if err != nil {
		return nil, err
	}
	return []byte("data: " + string(body) + "\n\n"), nil
}

func (i *ChatInbound) TransformStreamEvents(ctx context.Context, events []model.StreamEvent) ([]byte, error) {
	// 流式下签名挂在 tool call 事件上，直接读原始事件，不依赖聚合结果是否保留该字段。
	for idx := range events {
		compat.SaveGeminiToolCallSignature(events[idx].ToolCall)
	}
	stream := model.InternalResponseFromStreamEvents(events)
	if stream == nil {
		return nil, nil
	}
	return i.TransformStream(ctx, stream)
}

// GetInternalResponse returns the complete internal response for logging, statistics, etc.
// For streaming: aggregates all stored stream chunks into a complete response
// For non-streaming: returns the stored response
func (i *ChatInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	if i.storedResponse != nil {
		return i.storedResponse, nil
	}
	return i.streamAggregator.BuildAndReset(), nil
}
