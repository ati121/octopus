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

const systemOneTestResponse = `{"model":"jev-1.13.0","answers":{"is_urgent":{"type":"noul","noul":0.95},"department":{"type":"choice","choice":"billing","probabilities":{"billing":0.88,"technical":0.12},"confidence":0.81}},"usage":{"input_tokens":392,"output_tokens":65}}`

type systemOneUpstreamCall struct {
	path          string
	authorization string
	body          map[string]json.RawMessage
}

func newSystemOneTestUpstream(t *testing.T, calls chan<- systemOneUpstreamCall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		calls <- systemOneUpstreamCall{path: r.URL.Path, authorization: r.Header.Get("Authorization"), body: body}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/systemone") {
			_, _ = w.Write([]byte(systemOneTestResponse))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
}

func createSystemOneTestGroup(t *testing.T, name string, channelID int, modelName string) {
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

func serveSystemOneTestRequest(groupName string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	requestBody := `{"model":"` + groupName + `","state":"Help! My payouts have been failing for 3 days.","questions":{"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"},"department":{"type":"choice","instructions":"Which team?","criteria":{"billing":"Payments","technical":null}}}}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeSystemOne, c)
	return recorder
}

func TestHandlerRelaysSystemOneRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	calls := make(chan systemOneUpstreamCall, 4)
	server := newSystemOneTestUpstream(t, calls)
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-systemone",
		Type:     outbound.OutboundTypeSystemOne,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "jev-latest",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "ts-test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createSystemOneTestGroup(t, "relay-systemone-group", channel.ID, "jev-latest")

	recorder := serveSystemOneTestRequest("relay-systemone-group")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected system one request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	var actual, expected any
	if err := json.Unmarshal(recorder.Body.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(systemOneTestResponse), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("expected upstream answers to be returned unchanged, got %s", recorder.Body.String())
	}

	var call systemOneUpstreamCall
	select {
	case call = <-calls:
	default:
		t.Fatal("upstream was not called")
	}
	if call.path != "/v1/systemone" {
		t.Fatalf("expected upstream path /v1/systemone, got %s", call.path)
	}
	if call.authorization != "Bearer ts-test-key" {
		t.Fatalf("unexpected upstream Authorization header: %q", call.authorization)
	}
	var routedModel string
	if err := json.Unmarshal(call.body["model"], &routedModel); err != nil || routedModel != "jev-latest" {
		t.Fatalf("expected group model to be rewritten to jev-latest, got %s (err=%v)", call.body["model"], err)
	}
	var questions map[string]json.RawMessage
	if err := json.Unmarshal(call.body["questions"], &questions); err != nil || len(questions) != 2 {
		t.Fatalf("expected questions to be forwarded, got %s (err=%v)", call.body["questions"], err)
	}
	if _, ok := call.body["state"]; !ok {
		t.Fatal("expected state to be forwarded")
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
	if relayLog.ActualModelName != "jev-1.13.0" {
		t.Fatalf("expected versioned model from upstream response, got %q", relayLog.ActualModelName)
	}
	if relayLog.Protocol != "SystemOne" {
		t.Fatalf("expected SystemOne protocol, got %q", relayLog.Protocol)
	}
	if relayLog.InputTokens != 392 || relayLog.OutputTokens != 65 {
		t.Fatalf("unexpected token usage: input=%d output=%d", relayLog.InputTokens, relayLog.OutputTokens)
	}
}

// 网关渠道（如 OpenRouter）里单独把 jev 设为 System One：该模型走 /systemone，
// System One 请求也不会被转发到按 Chat 协议配置的模型。
func TestHandlerRoutesSystemOneByModelType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	calls := make(chan systemOneUpstreamCall, 4)
	server := newSystemOneTestUpstream(t, calls)
	defer server.Close()

	channel := &model.Channel{
		Name:       "relay-gateway-with-jev",
		Type:       outbound.OutboundTypeOpenAIChat,
		Enabled:    true,
		BaseUrls:   []model.BaseUrl{{URL: server.URL + "/api/v1"}},
		Model:      "gpt-4o,~typesafe/jev-latest",
		ModelTypes: model.ChannelModelTypes{"~typesafe/jev-latest": outbound.OutboundTypeSystemOne},
		Keys:       []model.ChannelKey{{Enabled: true, ChannelKey: "gateway-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createSystemOneTestGroup(t, "relay-gateway-jev", channel.ID, "~typesafe/jev-latest")
	createSystemOneTestGroup(t, "relay-gateway-chat", channel.ID, "gpt-4o")

	recorder := serveSystemOneTestRequest("relay-gateway-jev")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected per-model system one request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	select {
	case call := <-calls:
		if call.path != "/api/v1/systemone" {
			t.Fatalf("expected upstream path /api/v1/systemone, got %s", call.path)
		}
		var routedModel string
		if err := json.Unmarshal(call.body["model"], &routedModel); err != nil || routedModel != "~typesafe/jev-latest" {
			t.Fatalf("expected gateway model name to be forwarded, got %s (err=%v)", call.body["model"], err)
		}
	default:
		t.Fatal("upstream was not called")
	}

	recorder = serveSystemOneTestRequest("relay-gateway-chat")
	if recorder.Code == http.StatusOK {
		t.Fatalf("expected system one request to a chat-typed model to fail, got body %s", recorder.Body.String())
	}
	select {
	case call := <-calls:
		t.Fatalf("system one request must not be forwarded to a chat-typed model, upstream got %s", call.path)
	default:
	}
}

func TestHandlerReturnsSystemOneValidationDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"loc":["body","questions","department","criteria"],"msg":"Field required","type":"missing"}]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-systemone-422",
		Type:     outbound.OutboundTypeSystemOne,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "jev-latest",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "ts-test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	createSystemOneTestGroup(t, "relay-systemone-422-group", channel.ID, "jev-latest")

	recorder := serveSystemOneTestRequest("relay-systemone-422-group")

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected upstream 422 to be returned, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "body.questions.department.criteria: Field required") {
		t.Fatalf("expected upstream validation detail in response, got %s", recorder.Body.String())
	}
}
