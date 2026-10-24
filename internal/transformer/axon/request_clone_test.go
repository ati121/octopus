package axon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestNativeRequestCopiesMutablePayloads(t *testing.T) {
	stream := true
	text := "hello"
	src := &llm.Request{
		Model: "group", Stream: &stream,
		Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{
			{Type: "image_url", ImageURL: &llm.ImageURL{URL: "data:image/png;base64,AAAA"}},
			{Type: "text", Text: &text, TransformerMetadata: map[string]any{"raw": json.RawMessage(`{"ok":true}`)}},
		}}}},
		TransformerMetadata: map[string]any{"nested": []any{map[string]any{"bytes": []byte("original")}}},
		RawRequest:          &httpclient.Request{Body: []byte(`{"original":true}`), Headers: http.Header{"X-Test": {"original"}}, Auth: &httpclient.AuthConfig{APIKey: "original"}},
		Image:               &llm.ImageRequest{ModelSpecified: true, Images: [][]byte{[]byte("image")}, Mask: []byte("mask")},
		Decisions:           &llm.DecisionsRequest{Body: []byte(`{"state":1}`)},
		ProviderExtensions: &llm.ProviderExtensions{OpenAIResponses: &llm.OpenAIResponsesProviderExtensions{Request: &llm.OpenAIResponsesRequestExtensions{
			RawFields:               map[string]json.RawMessage{"unknown": json.RawMessage(`{"ok":true}`)},
			RawInputItems:           []llm.OpenAIResponsesRawFragment{{Raw: json.RawMessage(`{"type":"message"}`)}},
			OmittedInputItemIndices: []int{2},
		}}},
	}
	view := &model.InternalLLMRequest{Native: src, Model: "upstream", Stream: &stream, Query: url.Values{"test": {"original"}}}
	snapshot := cloneNativeRequest(src)
	first, err := NativeRequest(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Image.ModelSpecified || !bytes.Equal(first.Decisions.Body, src.Decisions.Body) || len(first.ProviderExtensions.OpenAIResponses.Request.OmittedInputItemIndices) != 1 {
		t.Fatal("non-JSON payload fields were lost")
	}
	first.Messages[0].Content.MultipleContent[0].ImageURL.URL = "changed"
	*first.Messages[0].Content.MultipleContent[1].Text = "changed"
	first.Messages[0].Content.MultipleContent[1].TransformerMetadata["raw"].(json.RawMessage)[0] = ' '
	first.TransformerMetadata["nested"].([]any)[0].(map[string]any)["bytes"].([]byte)[0] = 'X'
	first.RawRequest.Body[0] = ' '
	first.RawRequest.Headers["X-Test"][0] = "changed"
	first.RawRequest.Auth.APIKey = "changed"
	first.RawRequest.Query["test"][0] = "changed"
	first.Image.Images[0][0] = 'X'
	first.Image.Mask[0] = 'X'
	first.Decisions.Body[0] = ' '
	first.ProviderExtensions.OpenAIResponses.Request.RawFields["unknown"][0] = ' '
	first.ProviderExtensions.OpenAIResponses.Request.RawInputItems[0].Raw[0] = ' '
	first.ProviderExtensions.OpenAIResponses.Request.OmittedInputItemIndices[0] = 3
	*first.Stream = false
	if !reflect.DeepEqual(src, snapshot) || !stream || view.Query.Get("test") != "original" {
		t.Fatal("attempt modified the authoritative request")
	}
	second, err := NativeRequest(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	if second.Messages[0].Content.MultipleContent[0].ImageURL.URL != "data:image/png;base64,AAAA" || second.RawRequest.Auth.APIKey != "original" {
		t.Fatal("retry inherited a prior attempt's changes")
	}
}

func TestNativeRequestWireMatchesJSONClone(t *testing.T) {
	inputs := []struct {
		kind int
		body string
	}{
		{0, `{"model":"group","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}],"tools":[{"type":"function","function":{"name":"test","parameters":{"type":"object","properties":{"value":{"type":"integer"}}}}}],"stream":true}`},
		{1, `{"model":"group","input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}],"tools":[{"type":"function","name":"test","parameters":{"type":"object"}}],"reasoning":{"effort":"high"},"stream":true}`},
		{2, `{"model":"group","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}],"tools":[{"name":"test","input_schema":{"type":"object"}}],"stream":true}`},
	}
	for _, input := range inputs {
		view, err := NewInbound(input.kind).TransformRequest(t.Context(), []byte(input.body))
		if err != nil {
			t.Fatal(err)
		}
		view.Model = "upstream"
		legacy, err := json.Marshal(view.Native)
		if err != nil {
			t.Fatal(err)
		}
		var old llm.Request
		if err := json.Unmarshal(legacy, &old); err != nil {
			t.Fatal(err)
		}
		old.ProviderExtensions = llm.CloneProviderExtensions(view.Native.ProviderExtensions)
		oldView := *view
		oldView.Native = &old
		for _, kind := range []int{0, 1, 2, 3, 4, 6} {
			actual, actualErr := NewOutbound(kind).WireRequest(t.Context(), view, "https://gateway.example/v1", "key")
			want, wantErr := NewOutbound(kind).WireRequest(t.Context(), &oldView, "https://gateway.example/v1", "key")
			if actualErr != nil || wantErr != nil {
				t.Fatalf("%d -> %d: actual %v; legacy %v", input.kind, kind, actualErr, wantErr)
			}
			var a, b any
			if err := json.Unmarshal(actual.Body, &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want.Body, &b); err != nil {
				t.Fatal(err)
			}
			if actual.URL != want.URL || !reflect.DeepEqual(a, b) {
				t.Errorf("%d -> %d: wire request changed", input.kind, kind)
			}
		}
	}
}

func TestNativeRequestIsolatesResponsesPreviousID(t *testing.T) {
	view, err := NewInbound(1).TransformRequest(t.Context(), []byte(`{"model":"group","input":"hello","previous_response_id":"resp_original"}`))
	if err != nil {
		t.Fatal(err)
	}
	first, err := NativeRequest(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	if first.PreviousResponseID == nil {
		t.Fatal("previous response ID was lost")
	}
	*first.PreviousResponseID = "resp_changed"
	if *view.PreviousResponseID != "resp_original" || *view.Native.PreviousResponseID != "resp_original" {
		t.Fatal("previous response ID leaked across attempts")
	}
}

func BenchmarkNativeRequestInlineMedia(b *testing.B) {
	body := []byte(`{"model":"group","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + strings.Repeat("A", 6<<20) + `"}}]}]}`)
	view, err := NewInbound(0).TransformRequest(b.Context(), body)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := NativeRequest(b.Context(), view); err != nil {
			b.Fatal(err)
		}
	}
}
