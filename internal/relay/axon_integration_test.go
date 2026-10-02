package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Exercise the real Handler, routing, AxonHub transformers and stats together.
func TestAxonHubEndpointsAndSiliconFlowModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	tests := []struct {
		name, path, upstreamModel, request, response, outputPath, output string
		in                                                               inbound.InboundType
		out                                                              outbound.OutboundType
		inputTokens, outputTokens                                        int
	}{
		{"chat", "/v1/chat/completions", "chat-native", `{"model":"group","messages":[{"role":"user","content":"hello"}]}`, `{"id":"chat-1","object":"chat.completion","model":"chat-native","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`, "choices.0.message.content", "hello", inbound.InboundTypeOpenAIChat, outbound.OutboundTypeOpenAIChat, 11, 3},
		{"responses", "/v1/responses", "response-native", `{"model":"group","input":"hello"}`, `{"id":"resp_1","object":"response","model":"response-native","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":11,"output_tokens":3,"total_tokens":14}}`, "output.0.content.0.text", "hello", inbound.InboundTypeOpenAIResponse, outbound.OutboundTypeOpenAIResponse, 11, 3},
		{"anthropic", "/v1/messages", "claude-native", `{"model":"group","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-native","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":3}}`, "content.0.text", "hello", inbound.InboundTypeAnthropic, outbound.OutboundTypeAnthropic, 11, 3},
		{"siliconflow-embedding", "/v1/embeddings", "BAAI/bge-m3", `{"model":"group","input":["hello","world"],"encoding_format":"float"}`, `{"object":"list","model":"BAAI/bge-m3","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]},{"object":"embedding","index":1,"embedding":[0.3,0.4]}],"usage":{"prompt_tokens":11,"total_tokens":11}}`, "data.1.embedding.1", "0.4", inbound.InboundTypeOpenAIEmbedding, outbound.OutboundTypeOpenAIEmbedding, 11, 0},
		{"siliconflow-rerank", "/v1/rerank", "BAAI/bge-reranker-v2-m3", `{"model":"group","query":"hello","documents":["hello","world"],"top_n":1,"return_documents":true}`, `{"model":"BAAI/bge-reranker-v2-m3","results":[{"index":0,"relevance_score":0.97,"document":{"text":"hello"}}],"usage":{"prompt_tokens":11,"total_tokens":11}}`, "results.0.document.text", "hello", inbound.InboundTypeRerank, outbound.OutboundTypeRerank, 11, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.URL.Path != tt.path {
					t.Errorf("upstream path %s, want %s", r.URL.Path, tt.path)
				}
				if tt.out == outbound.OutboundTypeAnthropic {
					if r.Header.Get("X-API-Key") != "upstream-key" {
						t.Error("missing Anthropic key")
					}
				} else if r.Header.Get("Authorization") != "Bearer upstream-key" {
					t.Error("missing bearer key")
				}
				b, _ := io.ReadAll(r.Body)
				if gjson.GetBytes(b, "model").String() != tt.upstreamModel {
					t.Errorf("routed model missing: %s", b)
				}
				if tt.in == inbound.InboundTypeOpenAIEmbedding && len(gjson.GetBytes(b, "input").Array()) != 2 {
					t.Errorf("embedding inputs lost: %s", b)
				}
				if tt.in == inbound.InboundTypeRerank && (gjson.GetBytes(b, "query").String() != "hello" || len(gjson.GetBytes(b, "documents").Array()) != 2) {
					t.Errorf("rerank inputs lost: %s", b)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.response)
			}))
			defer upstream.Close()
			channel := &model.Channel{Name: tt.name, Type: tt.out, Enabled: true, Model: tt.upstreamModel, BaseUrls: []model.BaseUrl{{URL: upstream.URL + "/v1"}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "upstream-key"}}}
			if err := op.ChannelCreate(channel, ctx); err != nil {
				t.Fatal(err)
			}
			groupName := "axon-" + tt.name
			createSystemOneTestGroup(t, groupName, channel.ID, tt.upstreamModel)
			var payload map[string]any
			if err := json.Unmarshal([]byte(tt.request), &payload); err != nil {
				t.Fatal(err)
			}
			payload["model"] = groupName
			b, _ := json.Marshal(payload)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("api_key_id", 7)
			c.Request = httptest.NewRequest("POST", tt.path, strings.NewReader(string(b)))
			c.Request.Header.Set("Content-Type", "application/json")
			Handler(tt.in, c)
			if !called || recorder.Code != 200 {
				t.Fatalf("request failed: called=%v status=%d body=%s", called, recorder.Code, recorder.Body.String())
			}
			if got := gjson.GetBytes(recorder.Body.Bytes(), tt.outputPath).String(); got != tt.output {
				t.Errorf("output %s = %q, want %q; body=%s", tt.outputPath, got, tt.output, recorder.Body.String())
			}
			logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 20)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range logs {
				if entry.RequestModelName == groupName {
					if !entry.Success || entry.InputTokens != tt.inputTokens || entry.OutputTokens != tt.outputTokens {
						t.Errorf("stats mismatch: success=%v tokens=%d/%d error=%s", entry.Success, entry.InputTokens, entry.OutputTokens, entry.Error)
					}
					return
				}
			}
			t.Fatal("missing relay log")
		})
	}
}
