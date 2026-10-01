package relay

import (
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

func TestHandlerSkipsModelRemovedFromChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	var staleCalls, availableCalls atomic.Int32
	staleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staleCalls.Add(1)
		http.Error(w, "removed model", http.StatusBadRequest)
	}))
	defer staleServer.Close()
	availableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		availableCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"step-5-preview","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer availableServer.Close()

	group := &model.Group{Name: "step-5-preview", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatal(err)
	}
	var staleChannelID int
	for i, url := range []string{staleServer.URL, availableServer.URL} {
		channel := &model.Channel{
			Name: url, Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
			BaseUrls: []model.BaseUrl{{URL: url + "/v1"}}, Model: "step-5-preview",
			Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
		}
		if err := op.ChannelCreate(channel, ctx); err != nil {
			t.Fatal(err)
		}
		if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: group.Name, Priority: i + 1, Weight: 1}, ctx); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			staleChannelID = channel.ID
		}
	}
	remainingModels := "other-model"
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: staleChannelID, Model: &remainingModels}, ctx); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"step-5-preview","messages":[{"role":"user","content":"hello"}],"max_tokens":16}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("request failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if staleCalls.Load() != 0 || availableCalls.Load() != 1 {
		t.Fatalf("upstream calls: removed=%d available=%d, want 0 and 1", staleCalls.Load(), availableCalls.Load())
	}
}
