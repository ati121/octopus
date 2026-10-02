package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// 即使请求声明了原生搜索工具，也不能吞掉上游返回的调用并触发网关重放。
func TestHandleResponseForwardsSearchCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req := &model.InternalLLMRequest{
		Model: "search-model",
		Tools: []model.Tool{{Type: "web_search"}},
	}
	ra := &relayAttempt{
		relayRequest: &relayRequest{
			c:               c,
			inAdapter:       inbound.Get(inbound.InboundTypeOpenAIChat),
			internalRequest: req,
			metrics:         NewRelayMetrics(1, req.Model, nil, req),
		},
		channel:    &dbmodel.Channel{Type: outbound.OutboundTypeOpenAIChat},
		outAdapter: outbound.Get(outbound.OutboundTypeOpenAIChat),
	}
	body := `{"id":"search-1","object":"chat.completion","model":"search-model","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_search","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"test query\"}"}}]},"finish_reason":"tool_calls"}]}`
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
	if err := ra.handleResponse(context.Background(), response); err != nil {
		t.Fatalf("search response was intercepted: %v", err)
	}
	var got struct {
		Choices []struct {
			Message struct {
				ToolCalls []model.ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || len(got.Choices) != 1 || len(got.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("missing search call: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	call := got.Choices[0].Message.ToolCalls[0]
	if call.ID != "call_search" || call.Function.Name != "web_search" || call.Function.Arguments != `{"query":"test query"}` {
		t.Fatalf("search call changed: %+v", call)
	}
}
