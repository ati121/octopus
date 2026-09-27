package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// 同一渠道里，单独设置了类型的模型按自己的协议转发，其余模型沿用渠道类型。
func TestHandlerRoutesModelByItsOwnChannelType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	paths := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/messages" {
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:       "relay-mixed-model-types",
		Type:       outbound.OutboundTypeOpenAIChat,
		Enabled:    true,
		BaseUrls:   []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:      "gpt-4o,claude-sonnet",
		ModelTypes: model.ChannelModelTypes{"claude-sonnet": outbound.OutboundTypeAnthropic},
		Keys:       []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	for _, tc := range []struct {
		modelName string
		wantPath  string
	}{
		{modelName: "claude-sonnet", wantPath: "/v1/messages"},
		{modelName: "gpt-4o", wantPath: "/v1/chat/completions"},
	} {
		group := &model.Group{Name: "relay-mixed-" + tc.modelName, Mode: model.GroupModeFailover}
		if err := op.GroupCreate(group, ctx); err != nil {
			t.Fatalf("GroupCreate failed: %v", err)
		}
		if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: tc.modelName, Priority: 1, Weight: 1}, ctx); err != nil {
			t.Fatalf("GroupItemAdd failed: %v", err)
		}

		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("api_key_id", 7)
		requestBody := `{"model":"` + group.Name + `","messages":[{"role":"user","content":"hello"}],"max_tokens":16}`
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
		c.Request.Header.Set("Content-Type", "application/json")

		Handler(inbound.InboundTypeOpenAIChat, c)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected request to succeed, got status %d body %s", tc.modelName, recorder.Code, recorder.Body.String())
		}
		select {
		case got := <-paths:
			if got != tc.wantPath {
				t.Fatalf("%s: expected upstream path %s, got %s", tc.modelName, tc.wantPath, got)
			}
		default:
			t.Fatalf("%s: upstream was not called", tc.modelName)
		}
	}
}
