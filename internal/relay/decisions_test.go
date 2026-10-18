package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

const decisionsTestResponse = `{"model":"gpt-6-luna","answers":[{"type":"choice","name":"department","choice":"billing"},{"type":"noul","name":"urgent","noul":0.91}],"usage":{"input_tokens":120,"output_tokens":7,"x_beta":"preserve me"},"x_beta":null}`

type decisionsUpstreamCall struct {
	path          string
	authorization string
	body          map[string]json.RawMessage
}

func newDecisionsTestUpstream(t *testing.T, calls chan<- decisionsUpstreamCall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		calls <- decisionsUpstreamCall{path: r.URL.Path, authorization: r.Header.Get("Authorization"), body: body}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/decisions") {
			_, _ = w.Write([]byte(decisionsTestResponse))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
}

func createDecisionsTestGroup(t *testing.T, name string, channelID int, modelName string) {
	t.Helper()
	ctx := t.Context()
	group := &model.Group{Name: name, Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channelID, ModelName: modelName, Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
}

func serveDecisionsTestRequest(groupName string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	requestBody := `{"model":"` + groupName + `","input":"I was charged twice for my order.","questions":[{"type":"choice","name":"department","instructions":"Which department?","choices":[{"value":"billing","description":"Payments."},{"value":true,"description":"A boolean choice must stay boolean."}],"x_beta":null}],"x_beta":{"ordered":["first","second"]}}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeDecisions, c)
	return recorder
}

func TestHandlerRelaysDecisionsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	calls := make(chan decisionsUpstreamCall, 4)
	server := newDecisionsTestUpstream(t, calls)
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-decisions",
		Type:     outbound.OutboundTypeDecisions,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "gpt-6-luna",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "oa-test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createDecisionsTestGroup(t, "relay-decisions-group", channel.ID, "gpt-6-luna")

	recorder := serveDecisionsTestRequest("relay-decisions-group")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected decisions request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	var actual, expected any
	if err := json.Unmarshal(recorder.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(decisionsTestResponse), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("expected upstream answers to be returned unchanged, got %s", recorder.Body.String())
	}

	var call decisionsUpstreamCall
	select {
	case call = <-calls:
	default:
		t.Fatal("upstream was not called")
	}
	if call.path != "/v1/decisions" {
		t.Fatalf("expected upstream path /v1/decisions, got %s", call.path)
	}
	if call.authorization != "Bearer oa-test-key" {
		t.Fatalf("unexpected upstream Authorization header: %q", call.authorization)
	}
	var routedModel string
	if err := json.Unmarshal(call.body["model"], &routedModel); err != nil || routedModel != "gpt-6-luna" {
		t.Fatalf("expected group model to be rewritten to gpt-6-luna, got %s (err=%v)", call.body["model"], err)
	}
	var questions []json.RawMessage
	if err := json.Unmarshal(call.body["questions"], &questions); err != nil || len(questions) != 1 {
		t.Fatalf("expected questions to be forwarded, got %s (err=%v)", call.body["questions"], err)
	}
	if _, ok := call.body["x_beta"]; !ok {
		t.Fatal("expected unknown request fields to be forwarded")
	}

	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("RelayLogList failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("expected relay log")
	}
	relayLog := logs[0]
	if !relayLog.Success {
		t.Fatalf("expected successful relay log, got error %q", relayLog.Error)
	}
	if relayLog.ActualModelName != "gpt-6-luna" {
		t.Fatalf("expected model from upstream response, got %q", relayLog.ActualModelName)
	}
	if relayLog.Protocol != "Decisions" {
		t.Fatalf("expected Decisions protocol, got %q", relayLog.Protocol)
	}
	// Decisions 只文档化 input_tokens；输出侧保持 0，不猜测上游未声明的用量。
	if relayLog.InputTokens != 120 || relayLog.OutputTokens != 0 {
		t.Fatalf("unexpected token usage: input=%d output=%d", relayLog.InputTokens, relayLog.OutputTokens)
	}
}

// Decisions 请求只会发给 Decisions 类型（或按模型单独设为 Decisions）的渠道。
func TestHandlerRoutesDecisionsByModelType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	calls := make(chan decisionsUpstreamCall, 4)
	server := newDecisionsTestUpstream(t, calls)
	defer server.Close()

	channel := &model.Channel{
		Name:       "relay-gateway-with-decisions",
		Type:       outbound.OutboundTypeOpenAIChat,
		Enabled:    true,
		BaseUrls:   []model.BaseUrl{{URL: server.URL + "/api/v1"}},
		Model:      "gpt-4o,gpt-6-luna",
		ModelTypes: model.ChannelModelTypes{"gpt-6-luna": outbound.OutboundTypeDecisions},
		Keys:       []model.ChannelKey{{Enabled: true, ChannelKey: "gateway-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createDecisionsTestGroup(t, "relay-gateway-decisions", channel.ID, "gpt-6-luna")
	createDecisionsTestGroup(t, "relay-gateway-chat-only", channel.ID, "gpt-4o")

	recorder := serveDecisionsTestRequest("relay-gateway-decisions")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected per-model decisions request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	select {
	case call := <-calls:
		if call.path != "/api/v1/decisions" {
			t.Fatalf("expected upstream path /api/v1/decisions, got %s", call.path)
		}
	default:
		t.Fatal("upstream was not called")
	}

	recorder = serveDecisionsTestRequest("relay-gateway-chat-only")
	if recorder.Code == http.StatusOK {
		t.Fatalf("expected decisions request to a chat-typed model to fail, got body %s", recorder.Body.String())
	}
	select {
	case call := <-calls:
		t.Fatalf("decisions request must not be forwarded to a chat-typed model, upstream got %s", call.path)
	default:
	}
}

func TestHandlerReturnsDecisionsUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"decision rate limit","type":"rate_limit_error"}}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-decisions-429",
		Type:     outbound.OutboundTypeDecisions,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "gpt-6-luna",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "oa-test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createDecisionsTestGroup(t, "relay-decisions-429-group", channel.ID, "gpt-6-luna")

	recorder := serveDecisionsTestRequest("relay-decisions-429-group")

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected upstream 429 to be returned, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "decision rate limit") {
		t.Fatalf("expected upstream error message in response, got %s", recorder.Body.String())
	}
}
