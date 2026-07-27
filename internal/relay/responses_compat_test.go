package relay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformermodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestShouldRetryWithoutResponsesItemReference(t *testing.T) {
	attempt := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: &transformermodel.InternalLLMRequest{}},
		channel: &dbmodel.Channel{
			ID:   7001,
			Name: "strict-responses",
			Type: outbound.OutboundTypeOpenAIResponse,
		},
	}

	if !attempt.shouldRetryWithoutResponsesItemReference(400, []byte(`{"error":{"message":"Unknown parameter: 'input[7].item_reference'."}}`)) {
		t.Fatal("expected unknown item_reference error to trigger compatibility retry")
	}
	if attempt.shouldRetryWithoutResponsesItemReference(400, []byte(`{"error":{"message":"Unknown parameter: 'input[7].other'."}}`)) {
		t.Fatal("unexpected retry for unrelated parameter")
	}
	if attempt.shouldRetryWithoutResponsesItemReference(503, []byte(`item_reference unknown parameter`)) {
		t.Fatal("unexpected retry for non-400 response")
	}
}

func TestResponsesItemReferenceCompatibilityCache(t *testing.T) {
	const channelID = 7002
	responsesItemReferenceUnsupported.Delete(channelID)
	t.Cleanup(func() { responsesItemReferenceUnsupported.Delete(channelID) })

	request := &transformermodel.InternalLLMRequest{}
	attempt := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: request},
		channel: &dbmodel.Channel{
			ID:   channelID,
			Name: "strict-responses",
			Type: outbound.OutboundTypeOpenAIResponse,
		},
	}

	markResponsesItemReferenceUnsupported(channelID)
	restore := attempt.applyResponsesItemReferenceCompatibility()
	if !request.TransformOptions.OmitResponsesItemReference {
		t.Fatal("expected cached channel capability to omit item_reference")
	}
	restore()
	if request.TransformOptions.OmitResponsesItemReference {
		t.Fatal("expected request transform option to be restored")
	}
}

func TestForwardViaHTTPStandardRetriesWithoutItemReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const channelID = 7003
	responsesItemReferenceUnsupported.Delete(channelID)
	t.Cleanup(func() { responsesItemReferenceUnsupported.Delete(channelID) })

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requestNumber := hits.Add(1)
		hasItemReference := strings.Contains(string(body), `"item_reference"`)
		if requestNumber == 1 {
			if !hasItemReference {
				t.Errorf("first request should preserve item_reference: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unknown parameter: 'input[1].item_reference'.","type":"invalid_request_error"}}`))
			return
		}
		if hasItemReference {
			t.Errorf("compatibility retry still contained item_reference: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"response","created_at":1,"model":"gpt-5.6-luna","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	stream := false
	toolCallID := "call_1"
	toolOutput := "done"
	internalRequest := &transformermodel.InternalLLMRequest{
		Model:  "gpt-5.6-luna",
		Stream: &stream,
		Messages: []transformermodel.Message{
			{
				Role: "assistant",
				ToolCalls: []transformermodel.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: transformermodel.FunctionCall{
						Name:      "lookup",
						Arguments: `{}`,
					},
				}},
			},
			{
				Role:       "tool",
				ToolCallID: &toolCallID,
				Content:    transformermodel.MessageContent{Content: &toolOutput},
			},
		},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	channel := &dbmodel.Channel{
		ID:       channelID,
		Name:     "strict-responses",
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []dbmodel.BaseUrl{{URL: server.URL + "/v1"}},
		Keys:     []dbmodel.ChannelKey{{ID: 1, Enabled: true, ChannelKey: "test-key"}},
	}
	relayRequest := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIChat),
		internalRequest: internalRequest,
		metrics:         NewRelayMetrics(1, internalRequest.Model, nil, internalRequest),
		requestModel:    internalRequest.Model,
	}
	attempt := &relayAttempt{
		relayRequest: relayRequest,
		outAdapter:   outbound.Get(channel.Type),
		channel:      channel,
		usedKey:      channel.Keys[0],
	}

	statusCode, err := attempt.forwardViaHTTPStandard(context.Background())
	if err != nil || statusCode != http.StatusOK {
		t.Fatalf("expected compatibility retry to succeed, status=%d err=%v", statusCode, err)
	}
	if hits.Load() != 2 {
		t.Fatalf("expected one compatibility retry, got %d requests", hits.Load())
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"content":"ok"`) {
		t.Fatalf("unexpected downstream response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if internalRequest.TransformOptions.OmitResponsesItemReference {
		t.Fatal("request compatibility option was not restored")
	}
}
