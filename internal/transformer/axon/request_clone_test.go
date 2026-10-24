package axon

import (
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

func TestNativeRequestIsolatesAttemptOptions(t *testing.T) {
	stream := true
	src := &llm.Request{Model: "upstream", Stream: &stream, StreamOptions: &llm.StreamOptions{},
		TransformerMetadata: map[string]any{"key": "original"},
		RawRequest:          &httpclient.Request{Headers: http.Header{"X-Test": {"original"}}}}
	view := &model.InternalLLMRequest{Request: *src, Query: url.Values{"test": {"original"}}}
	first, err := NativeRequest(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	first.TransformerMetadata["key"] = "changed"
	first.RawRequest.Headers.Set("X-Test", "changed")
	first.RawRequest.Query.Set("test", "changed")
	first.StreamOptions.IncludeUsage = true
	*first.Stream = false
	second, err := NativeRequest(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	if second.TransformerMetadata["key"] != "original" || second.RawRequest.Headers.Get("X-Test") != "original" || second.RawRequest.Query.Get("test") != "original" || second.StreamOptions.IncludeUsage || !*second.Stream {
		t.Fatal("attempt options leaked into retry")
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
		legacy, err := json.Marshal(&view.Request)
		if err != nil {
			t.Fatal(err)
		}
		var old llm.Request
		if err := json.Unmarshal(legacy, &old); err != nil {
			t.Fatal(err)
		}
		old.ProviderExtensions = llm.CloneProviderExtensions(view.ProviderExtensions)
		oldView := *view
		oldView.Request = old
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
	if *view.PreviousResponseID != "resp_original" {
		t.Fatal("previous response ID leaked across attempts")
	}
}

func TestNativeRequestRetainsQuery(t *testing.T) {
	for _, fromNative := range []bool{true, false} {
		view := &model.InternalLLMRequest{}
		query := url.Values{"test": {"original"}}
		if fromNative {
			view.RawRequest = &httpclient.Request{Headers: make(http.Header), Query: query}
		} else {
			view.Query = query
		}
		native, err := NativeRequest(t.Context(), view)
		if err != nil || native.RawRequest == nil || native.RawRequest.Query.Get("test") != "original" {
			t.Fatalf("query lost (native=%t): %v", fromNative, err)
		}
		native.RawRequest.Query.Set("test", "changed")
		if query.Get("test") != "original" {
			t.Fatal("query mutation leaked into source")
		}
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
