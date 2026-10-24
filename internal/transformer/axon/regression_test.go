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
				t.Fatalf("lost content: %s; raw=%s; native=%+v", body, response.RawResponsesOutputItems, response.Response)
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

func TestNativeRequestIsolatesTransformerAttempts(t *testing.T) {
	for _, input := range []struct {
		kind int
		body string
	}{
		{0, `{"model":"group","messages":[{"role":"user","content":"hello"}],"stream":true}`},
		{1, `{"model":"group","input":"hello","tools":[{"type":"web_search"}],"metadata":{"test":"original"},"stream":true}`},
		{2, `{"model":"group","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`},
	} {
		original, err := NewInbound(input.kind).TransformRequest(t.Context(), []byte(input.body))
		if err != nil {
			t.Fatal(err)
		}
		snapshot, _ := json.Marshal(original)
		for _, kind := range []int{0, 1, 2, 3, 4, 6, 0, 1} {
			_, err := NewOutbound(kind).WireRequest(t.Context(), original, "https://gateway.example/v1", "key")
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(original)
			if !bytes.Equal(snapshot, after) {
				t.Fatalf("transformer %d mutated input %d", kind, input.kind)
			}
		}
	}
}

func TestAnthropicCacheUsageProjection(t *testing.T) {
	out := NewOutbound(2)
	response, err := out.TransformResponse(context.Background(), &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`))})
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage == nil || response.Usage.PromptTokens != 35 || response.Usage.PromptTokensDetails.CachedTokens != 20 || response.Usage.PromptTokensDetails.WriteCachedTokens != 5 {
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

func TestStandardAuthDoesNotDependOnHost(t *testing.T) {
	for _, base := range []string{"https://token.sensenova.cn/v1", "https://gateway.example/v1"} {
		for _, kind := range []int{0, 2} {
			view, err := NewInbound(0).TransformRequest(t.Context(), []byte(`{"model":"group","messages":[{"role":"user","content":"hello"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			wire, err := NewOutbound(kind).WireRequest(t.Context(), view, base, "selected-key")
			if err != nil {
				t.Fatal(err)
			}
			request, err := HTTPRequest(t.Context(), wire)
			if err != nil {
				t.Fatal(err)
			}
			if kind == 2 {
				if request.Header.Get("X-Api-Key") != "selected-key" || request.Header.Get("Authorization") != "" {
					t.Fatalf("Anthropic auth changed for %s: %v", base, request.Header)
				}
			} else if request.Header.Get("Authorization") != "Bearer selected-key" || request.Header.Get("X-Api-Key") != "" {
				t.Fatalf("Chat auth changed for %s: %v", base, request.Header)
			}
		}
	}
}

// Keep the native wire request authoritative for non-chat entry points too.
var _ model.Inbound = (*Inbound)(nil)
var _ model.Outbound = (*Outbound)(nil)
