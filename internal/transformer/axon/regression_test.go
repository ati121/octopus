package axon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm"
	"github.com/tidwall/gjson"
)

func TestCodexSSEToNonStream(t *testing.T) {
	in := NewInbound(0)
	req, err := in.TransformRequest(t.Context(), []byte(`{"model":"group","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Model = "upstream"
	out := NewOutbound(6)
	if _, err := out.TransformRequest(t.Context(), req, "https://codex.example/v1", "key"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		wantErr    bool
	}{
		{"complete",
			"data: " + `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg-1","type":"message","role":"assistant"}}` + "\n\n" +
				"data: " + `{"type":"response.content_part.added","output_index":0,"content_index":0,"item_id":"msg-1","part":{"type":"output_text","text":""}}` + "\n\n" +
				"data: " + `{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg-1","delta":"answer"}` + "\n\n" +
				"data: " + `{"type":"response.completed","response":` + responsesAnswer + "}\n\n", false},
		{"truncated", "data: " + `{"type":"response.created","response":{"id":"resp-1","model":"upstream","status":"in_progress"}}` + "\n\n", true},
		{"error", "data: " + `{"type":"error","error":{"message":"upstream unavailable","type":"server_error"}}` + "\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := out.TransformResponse(t.Context(), &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))})
			if tc.wantErr {
				if err == nil {
					t.Fatal("incomplete/error stream accepted as success")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := in.TransformResponse(t.Context(), response)
			if err != nil {
				t.Fatal(err)
			}
			if gjson.GetBytes(body, "choices.0.message.content").String() != "answer" {
				t.Fatalf("lost content: %s; raw=%s; native=%+v", body, response.RawResponsesOutputItems, response.Native)
			}
			if response.Usage == nil || response.Usage.PromptTokens != 12 || response.Usage.CompletionTokens != 3 {
				t.Fatalf("lost usage: %+v", response.Usage)
			}
			if gjson.GetBytes(response.RawResponsesOutputItems, "0.id").String() != "msg-1" {
				t.Fatal("lost replay output items during usage aggregation")
			}
		})
	}
}

func TestNativeRequestIsolatesAttempts(t *testing.T) {
	in := NewInbound(1)
	original, err := in.TransformRequest(t.Context(), []byte(`{"model":"group","input":"hello","tools":[{"type":"web_search"}],"metadata":{"test":"original"}}`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(original.Native)
	private := llm.CloneProviderExtensions(original.Native.ProviderExtensions)
	first, err := NativeRequest(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	first.Messages[0].Role = "assistant"
	first.Metadata["test"] = "changed"
	if first.RawRequest != nil {
		first.RawRequest.Body[0] = ' '
	}
	if first.ProviderExtensions != nil && first.ProviderExtensions.OpenAIResponses != nil {
		first.ProviderExtensions.OpenAIResponses.Request.RawTools = nil
	}
	after, _ := json.Marshal(original.Native)
	if !bytes.Equal(snapshot, after) {
		t.Fatal("attempt mutated shared request")
	}
	if private != nil && len(private.OpenAIResponses.Request.RawTools) != len(original.Native.ProviderExtensions.OpenAIResponses.Request.RawTools) {
		t.Fatal("attempt mutated private provider fields")
	}
}

func TestAnthropicCacheUsageProjection(t *testing.T) {
	out := NewOutbound(2)
	response, err := out.TransformResponse(context.Background(), &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`))})
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage == nil || response.Usage.EffectiveInputTokens() != 35 || response.Usage.BillableCacheReadInput() != 20 || response.Usage.BillableCacheWriteInput() != 5 {
		t.Fatalf("cache accounting changed: %+v", response.Usage)
	}
}

func TestNativeProviderExtensionsSurviveProjection(t *testing.T) {
	in := NewInbound(1)
	view, err := in.TransformRequest(t.Context(), []byte(`{"model":"group","input":"hello","tools":[{"type":"web_search"}],"reasoning":{"summary":"auto"}}`))
	if err != nil {
		t.Fatal(err)
	}
	out := NewOutbound(1)
	request, err := out.TransformRequest(t.Context(), view, "https://example.com/v1", "key")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(request.Body)
	if gjson.GetBytes(body, "tools.0.type").String() != "web_search" {
		t.Fatalf("native tool lost: %s", body)
	}
}

// Keep the native wire request authoritative for non-chat entry points too.
var _ model.Inbound = (*Inbound)(nil)
var _ model.Outbound = (*Outbound)(nil)
