package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerGeminiMidstreamErrorIsFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"responseId":"test-response","modelVersion":"gemini-test","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"partial answer"}]}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: "+`{"error":{"code":503,"message":"The model is overloaded.","status":"UNAVAILABLE"}}`+"\n\n")
	}))
	defer upstream.Close()
	channel := &model.Channel{
		Name: "gemini-error", Type: outbound.OutboundTypeGemini, Enabled: true, Model: "gemini-test",
		BaseUrls: []model.BaseUrl{{URL: upstream.URL}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatal(err)
	}
	createSystemOneTestGroup(t, "gemini-error", channel.ID, "gemini-test")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gemini-error","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)
	body := recorder.Body.String()
	if !strings.Contains(body, "partial answer") || strings.Contains(body, "[DONE]") {
		t.Fatalf("上游错误后的流不应发送正常完成标记: %s", body)
	}
	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range logs {
		if entry.RequestModelName == "gemini-error" {
			if entry.Success || !strings.Contains(entry.Error, "overloaded") || len(entry.Attempts) != 1 || entry.Attempts[0].Status != model.AttemptFailed {
				t.Fatalf("Gemini 错误应记为失败: success=%t error=%q", entry.Success, entry.Error)
			}
			return
		}
	}
	t.Fatal("缺少请求日志")
}

func TestHandlerContentFilterDoesNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"test-filter","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":null},"finish_reason":"content_filter"}]}`)
	}))
	defer upstream.Close()
	group := &model.Group{Name: "content-filter", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"filter-primary", "filter-fallback"} {
		channel := &model.Channel{
			Name: name, Type: outbound.OutboundTypeOpenAIChat, Enabled: true, Model: "test-model",
			BaseUrls: []model.BaseUrl{{URL: upstream.URL + "/v1"}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		}
		if err := op.ChannelCreate(channel, ctx); err != nil {
			t.Fatal(err)
		}
		if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "test-model", Priority: channel.ID, Weight: 1}, ctx); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"content-filter","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)
	if recorder.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(recorder.Body.String(), `"finish_reason":"content_filter"`) {
		t.Fatalf("合法过滤终态不应触发重试或 502: calls=%d HTTP=%d body=%s", calls.Load(), recorder.Code, recorder.Body.String())
	}
	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range logs {
		if entry.RequestModelName == "content-filter" {
			if !entry.Success || len(entry.Attempts) != 1 || entry.Attempts[0].Status != model.AttemptSuccess {
				t.Fatal("合法过滤终态应一次成功，不应计为渠道故障")
			}
			return
		}
	}
	t.Fatal("缺少请求日志")
}
