package model

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/looplj/axonhub/llm"
)

type APIFormat = llm.APIFormat

const (
	APIFormatOpenAIChatCompletion = llm.APIFormatOpenAIChatCompletion
	APIFormatOpenAIResponse       = llm.APIFormatOpenAIResponse
	APIFormatOpenAIEmbedding      = llm.APIFormatOpenAIEmbedding
	APIFormatRerank               = llm.APIFormatJinaRerank
	APIFormatSystemOne            = llm.APIFormatTypeSafeSystemOne
	APIFormatOpenAIDecisions      = llm.APIFormatOpenAIDecisions
	APIFormatGeminiContents       = llm.APIFormatGeminiContents
	APIFormatAnthropicMessage     = llm.APIFormatAnthropicMessage
)

// TransformerMetadata keys that Octopus itself reads or writes. Provider
// metadata keys are AxonHub's concern and are not mirrored here.
const (
	TransformerMetadataWSExecutionMode            = "octopus_ws_execution_mode"
	TransformerMetadataWSExecutionModeReplayExact = "replay_exact"
)

// Protocol fields are embedded directly from AxonHub. Only relay/session state lives here.
type InternalLLMRequest struct {
	llm.Request
	RawBody       []byte          `json:"-"`
	Query         url.Values      `json:"-"`
	RawInputItems json.RawMessage `json:"-"`
	ReplayExact   bool            `json:"-"`
}

type InternalLLMResponse struct {
	llm.Response
	RawResponsesOutputItems json.RawMessage `json:"-"`
}

type Request = llm.Request
type Response = llm.Response
type Usage = llm.Usage

func (r *InternalLLMRequest) Validate() error {
	if r == nil || strings.TrimSpace(r.Model) == "" {
		return errors.New("model is required")
	}
	return nil
}
func (r *InternalLLMRequest) IsEmbeddingRequest() bool { return r.Embedding != nil }
func (r *InternalLLMRequest) IsRerankRequest() bool    { return r.Rerank != nil }
func (r *InternalLLMRequest) IsSystemOneRequest() bool { return r.SystemOne != nil }
func (r *InternalLLMRequest) IsDecisionsRequest() bool { return r.Decisions != nil }
func (r *InternalLLMRequest) IsChatRequest() bool {
	return r.RequestType == llm.RequestTypeChat || r.RequestType == "" && !r.IsEmbeddingRequest() && !r.IsRerankRequest() && !r.IsSystemOneRequest() && !r.IsDecisionsRequest()
}
func (r *InternalLLMRequest) IsOpenAIExactReplayRequest() bool { return r != nil && r.ReplayExact }
func (r *InternalLLMRequest) MarkOpenAIExactReplayRequest() {
	r.ReplayExact = true
	if r.TransformerMetadata == nil {
		r.TransformerMetadata = make(map[string]any)
	}
	r.TransformerMetadata[TransformerMetadataWSExecutionMode] = TransformerMetadataWSExecutionModeReplayExact
}
func (r *InternalLLMResponse) IsEmbeddingResponse() bool { return r != nil && r.Embedding != nil }
func (r *InternalLLMResponse) IsRerankResponse() bool    { return r != nil && r.Rerank != nil }
func (r *InternalLLMResponse) IsSystemOneResponse() bool { return r != nil && r.SystemOne != nil }
func (r *InternalLLMResponse) IsDecisionsResponse() bool { return r != nil && r.Decisions != nil }

type Annotation = llm.Annotation
type CacheControl = llm.CacheControl
type Choice = llm.Choice
type CompletionTokensDetails = llm.CompletionTokensDetails
type Embedding = llm.Embedding
type EmbeddingInput = llm.EmbeddingInput
type ErrorDetail = llm.ErrorDetail
type Function = llm.Function
type FunctionCall = llm.FunctionCall
type ImageGeneration = llm.ImageGeneration
type ImageURL = llm.ImageURL
type LogprobsContent = llm.LogprobsContent
type Message = llm.Message
type MessageContent = llm.MessageContent
type MessageContentPart = llm.MessageContentPart
type ModalityTokenCount = llm.ModalityTokenCount
type NamedToolChoice = llm.NamedToolChoice
type PromptTokensDetails = llm.PromptTokensDetails
type ResponseError = llm.ResponseError
type ResponseFormat = llm.ResponseFormat
type ResponseMeta = llm.ResponseMeta
type Stop = llm.Stop
type StreamOptions = llm.StreamOptions
type TokenLogprob = llm.TokenLogprob
type Tool = llm.Tool
type ToolCall = llm.ToolCall
type ToolChoice = llm.ToolChoice
type ToolFunction = llm.ToolFunction
type TopLogprob = llm.TopLogprob
type TransformOptions = llm.TransformOptions
type URLCitation = llm.URLCitation
