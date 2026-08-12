package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// 回归：opencode 风格上游缺失 done 事件时，Responses 透传流必须在
// response.completed 前自动补发 function_call_arguments.done 与
// output_item.done，让只从 output_item.done 收集工具调用的客户端
// （如 Hermes）不丢掉工具调用、回合不提前结束。
func TestHandleStreamResponsePassthroughSynthesizesMissingDoneEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rawSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":\"beijing\"}"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"completed"}}`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	internalReq := &transformerModel.InternalLLMRequest{
		Model:        "gpt-4o",
		Stream:       boolPtr(true),
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, internalReq.Model, nil, internalReq),
		apiKeyID:        1,
		requestModel:    internalReq.Model,
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(outbound.OutboundTypeOpenAIResponse),
		channel:      &model.Channel{Type: outbound.OutboundTypeOpenAIResponse},
	}

	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(rawSSE))),
	}

	pt := ra.outAdapter.(transformerModel.PassthroughCapable)
	cfg := pt.PassthroughConfig()
	if err := ra.handleStreamResponsePassthroughV2(context.Background(), response, cfg); err != nil {
		t.Fatalf("handleStreamResponsePassthroughV2() error = %v", err)
	}

	got := recorder.Body.String()

	// 上游原始事件必须保留。
	for _, fragment := range []string{
		`"type":"response.created"`,
		`"type":"response.output_item.added"`,
		`"type":"response.function_call_arguments.delta"`,
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("original event %s lost in passthrough stream: %s", fragment, got)
		}
	}

	// 合成事件补齐且顺序正确：arguments.done < item.done < completed。
	argsDoneIdx := strings.Index(got, `"type":"response.function_call_arguments.done"`)
	itemDoneIdx := strings.Index(got, `"type":"response.output_item.done"`)
	completedIdx := strings.Index(got, `"type":"response.completed"`)
	if argsDoneIdx < 0 || itemDoneIdx < 0 {
		t.Fatalf("expected synthesized done events in passthrough stream, got: %s", got)
	}
	if !(argsDoneIdx < itemDoneIdx && itemDoneIdx < completedIdx) {
		t.Fatalf("synthesized done events must precede response.completed, got: %s", got)
	}

	// 合成载荷内容：身份、全量 arguments、completed 状态。
	for _, fragment := range []string{
		`"item_id":"fc_1"`,
		`"arguments":"{\"city\":\"beijing\"}"`,
		`"call_id":"call_1"`,
		`"name":"get_weather"`,
		`"status":"completed"`,
	} {
		if !strings.Contains(got, fragment) {
			t.Fatalf("synthesized payload missing %s, got: %s", fragment, got)
		}
	}
}

// 上游已发完整 done 事件（基元律动风格）时，透传输出必须与输入逐字节一致，
// 既不能重复合成，也不能改动原始帧。
func TestHandleStreamResponsePassthroughDoesNotDuplicateDoneEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rawSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"in_progress"}}`,
		"",
		`event: response.custom_debug`,
		`data: {"type":"response.custom_debug","custom":{"keep":true}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"city\":\"beijing\"}"}`,
		"",
		`data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{\"city\":\"beijing\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"beijing\"}","status":"completed"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"completed"}}`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	internalReq := &transformerModel.InternalLLMRequest{
		Model:        "gpt-4o",
		Stream:       boolPtr(true),
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, internalReq.Model, nil, internalReq),
		apiKeyID:        1,
		requestModel:    internalReq.Model,
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(outbound.OutboundTypeOpenAIResponse),
		channel:      &model.Channel{Type: outbound.OutboundTypeOpenAIResponse},
	}

	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(rawSSE))),
	}

	pt := ra.outAdapter.(transformerModel.PassthroughCapable)
	cfg := pt.PassthroughConfig()
	if err := ra.handleStreamResponsePassthroughV2(context.Background(), response, cfg); err != nil {
		t.Fatalf("handleStreamResponsePassthroughV2() error = %v", err)
	}
	if got := recorder.Body.String(); got != rawSSE {
		t.Fatalf("complete upstream must be forwarded byte-for-byte, got %q want %q", got, rawSSE)
	}
}
