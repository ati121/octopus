package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/axon"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestPipelineResponseBodyLimit(t *testing.T) {
	for _, tc := range []struct {
		body  string
		large bool
	}{{"1234", false}, {"12345", true}} {
		reader := &boundedResponseBody{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), remaining: 4, limit: 4}
		body, err := io.ReadAll(reader)
		if tc.large {
			if !iolimit.IsTooLarge(err) {
				t.Fatalf("oversized body accepted: %q %v", body, err)
			}
		} else if err != nil || string(body) != tc.body {
			t.Fatalf("exact limit rejected: %q %v", body, err)
		}
	}
}

func TestPipelineRejectsTruncatedResponsesAfterDeliveredContent(t *testing.T) {
	ra, recorder := newEmptyStreamTestAttempt(t, inbound.InboundTypeOpenAIChat, llm.APIFormatOpenAIChatCompletion, outbound.OutboundTypeOpenAIResponse)
	body := "data: " + `{"type":"response.output_text.delta","delta":"hello"}` + "\n\n"
	err := ra.handleStreamResponseV2(t.Context(), sseTestResponse(body))
	if !errors.Is(err, llm.ErrStreamIncomplete) {
		t.Fatalf("truncated Responses stream accepted: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "hello") {
		t.Fatal("content was not streamed")
	}
}

func TestPipelineBufferLimitFailsBeforeCommit(t *testing.T) {
	request, err := axon.NewInbound(0).TransformRequest(t.Context(), []byte(`{"model":"test","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	out := axon.NewOutbound(0)
	if err := out.Configure("https://unused.invalid/v1", "key"); err != nil {
		t.Fatal(err)
	}
	response := sseTestResponse("data: " + `{"choices":[{"index":0,"delta":{"content":"hello"}}]}` + "\n\n")
	native, err := axon.NativeRequest(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = axon.Execute(t.Context(), axon.NewInbound(0).Transformer, out, native, responseExecutor{Response: response}, 8, nil)
	if err == nil || !strings.Contains(err.Error(), "buffer limit") {
		t.Fatalf("buffer limit not enforced: %v", err)
	}
}

type nilEventStream struct{ index int }

func (s *nilEventStream) Next() bool { s.index++; return s.index <= 2 }
func (s *nilEventStream) Current() *httpclient.StreamEvent {
	if s.index == 1 {
		return nil
	}
	return &httpclient.StreamEvent{Data: []byte(`{"hello":true}`)}
}
func (*nilEventStream) Err() error   { return nil }
func (*nilEventStream) Close() error { return nil }

func TestPipelineWriterSkipsNilEvent(t *testing.T) {
	ra, recorder := newEmptyStreamTestAttempt(t, inbound.InboundTypeOpenAIChat, llm.APIFormatOpenAIChatCompletion, outbound.OutboundTypeOpenAIChat)
	if err := ra.writePipelineStream(context.Background(), &nilEventStream{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorder.Body.String(), `{"hello":true}`) {
		t.Fatal("nil event truncated stream")
	}
}

func TestPipelineWriterPropagatesReadError(t *testing.T) {
	ra, _ := newEmptyStreamTestAttempt(t, inbound.InboundTypeOpenAIChat, llm.APIFormatOpenAIChatCompletion, outbound.OutboundTypeOpenAIChat)
	source := streams.MapErr(streams.SliceStream([]int{1}), func(int) (*httpclient.StreamEvent, error) { return nil, io.ErrUnexpectedEOF })
	if err := ra.writePipelineStream(t.Context(), source); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error lost: %v", err)
	}
}

func TestProviderErrorDoesNotCountAsCompletedFromUsage(t *testing.T) {
	metrics := &RelayMetrics{}
	metrics.SetInternalResponse(&transformerModel.InternalLLMResponse{Response: llm.Response{Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 2}, Error: &llm.ResponseError{StatusCode: http.StatusBadGateway}}}, "test")
	if metricsSuggestCompletedStream(metrics) || streamResponseCompleted(metrics.InternalResponse) {
		t.Fatal("provider error counted as successful completion")
	}
}

func TestPipelineFallbackCollectsNewAttemptResponse(t *testing.T) {
	ra, _ := newEmptyStreamTestAttempt(t, inbound.InboundTypeOpenAIChat, llm.APIFormatOpenAIChatCompletion, outbound.OutboundTypeOpenAIChat)
	if err := ra.pipelineResponse(t.Context(), sseTestResponse("")); err == nil {
		t.Fatal("empty first attempt accepted")
	}
	body := "data: " + `{"choices":[{"index":0,"delta":{"content":"answer"}}]}` + "\n\n" +
		"data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\n\n" +
		"data: [DONE]\n\n"
	if err := ra.pipelineResponse(t.Context(), sseTestResponse(body)); err != nil {
		t.Fatal(err)
	}
	if ra.metrics.InternalResponse == nil || ra.metrics.Stats.InputToken != 10 || ra.metrics.Stats.OutputToken != 2 || len(ra.deferredStreamPayload) != 0 {
		t.Fatalf("fallback reused failed attempt state: %+v", ra.metrics.InternalResponse)
	}
}
