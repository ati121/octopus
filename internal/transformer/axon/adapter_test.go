package axon

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/tidwall/gjson"
)

const chatAnswer = `{"id":"chat-1","object":"chat.completion","model":"upstream","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`
const responsesAnswer = `{"id":"resp-1","object":"response","model":"upstream","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}`
const anthropicAnswer = `{"id":"msg-1","type":"message","model":"upstream","role":"assistant","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}`
const geminiAnswer = `{"modelVersion":"upstream","candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"totalTokenCount":15}}`

func TestConversationProtocolMatrix(t *testing.T) {
	inputs := []string{`{"model":"group","messages":[{"role":"user","content":"hello"}]}`, `{"model":"group","input":"hello"}`, `{"model":"group","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`}
	outputs := []struct {
		kind       int
		path, body string
	}{{0, "/v1/chat/completions", chatAnswer}, {1, "/v1/responses", responsesAnswer}, {2, "/v1/messages", anthropicAnswer}, {3, "/v1beta/models/upstream:generateContent", geminiAnswer}, {4, "/v3/chat/completions", chatAnswer}, {6, "/v1/responses", responsesAnswer}}
	for kind, body := range inputs {
		for _, outCase := range outputs {
			t.Run(fmt.Sprintf("%d_to_%d", kind, outCase.kind), func(t *testing.T) {
				ctx := t.Context()
				in := NewInbound(kind)
				request, err := in.TransformRequest(ctx, []byte(body))
				if err != nil {
					t.Fatal(err)
				}
				if request.Native == nil {
					t.Fatal("missing authoritative AxonHub request")
				}
				request.Model = "upstream"
				out := NewOutbound(outCase.kind)
				wire, err := out.TransformRequest(ctx, request, "https://gateway.example", "upstream-key")
				if err != nil {
					t.Fatal(err)
				}
				if wire.URL.Path != outCase.path {
					t.Errorf("path %s, want %s", wire.URL.Path, outCase.path)
				}
				raw, _ := io.ReadAll(wire.Body)
				if !bytes.Contains(raw, []byte("hello")) {
					t.Fatalf("lost input: %s", raw)
				}
				response, err := out.TransformResponse(ctx, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(outCase.body))})
				if err != nil {
					t.Fatal(err)
				}
				clientBody, err := in.TransformResponse(ctx, response)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(clientBody, []byte("answer")) {
					t.Fatalf("lost output: %s", clientBody)
				}
				if response.Usage == nil || response.Usage.PromptTokens != 12 || response.Usage.CompletionTokens != 3 {
					t.Fatalf("lost usage: %+v", response.Usage)
				}
			})
		}
	}
}

func TestImageEndpointsUseAxonHub(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/images/generations", "/images/edits", "/images/variations"} {
		t.Run(endpoint, func(t *testing.T) {
			in, err := ImageInbound(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(`{"model":"group","prompt":"draw a tree","response_format":"b64_json"}`)
			ct := "application/json"
			if endpoint != "/images/generations" {
				var b bytes.Buffer
				mw := multipart.NewWriter(&b)
				_ = mw.WriteField("model", "group")
				if endpoint == "/images/edits" {
					_ = mw.WriteField("prompt", "draw a tree")
				}
				file, err := mw.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="image"; filename="test.png"`}, "Content-Type": {"image/png"}})
				if err != nil {
					t.Fatal(err)
				}
				_, _ = file.Write(imageData.Bytes())
				_ = mw.Close()
				ct = mw.FormDataContentType()
				body = b.Bytes()
			}
			native, err := in.TransformRequest(t.Context(), &httpclient.Request{Method: "POST", Headers: http.Header{"Content-Type": {ct}}, ContentType: ct, Body: body})
			if err != nil {
				t.Fatal(err)
			}
			out := NewOutbound(0)
			wire, err := out.TransformRequest(t.Context(), &model.InternalLLMRequest{Model: "image-native", Native: native}, "https://gateway.example/v1", "key")
			if err != nil {
				t.Fatal(err)
			}
			if wire.URL.Path != "/v1"+endpoint {
				t.Fatalf("image path %s", wire.URL.Path)
			}
			if wire.Header.Get("Authorization") != "Bearer key" {
				t.Fatal("missing image key")
			}
			if endpoint != "/images/generations" {
				if err := wire.ParseMultipartForm(1 << 20); err != nil {
					t.Fatal(err)
				}
				defer wire.MultipartForm.RemoveAll()
				if wire.FormValue("model") != "image-native" {
					t.Fatal("image routed model lost")
				}
				file, _, err := wire.FormFile("image")
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(file)
				_ = file.Close()
				if !bytes.Equal(b, imageData.Bytes()) {
					t.Fatal("image bytes changed")
				}
			}
			raw := []byte(`{"created":1,"data":[{"b64_json":"aGVsbG8=","revised_prompt":"tree"}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
			response, err := out.Transformer.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: raw, Request: out.Request})
			if err != nil {
				t.Fatal(err)
			}
			client, err := in.TransformResponse(t.Context(), response)
			if err != nil {
				t.Fatal(err)
			}
			if gjson.GetBytes(client.Body, "data.0.b64_json").String() != "aGVsbG8=" {
				t.Fatalf("image data lost: %s", client.Body)
			}
		})
	}
	in, _ := ImageInbound("/images/generations")
	_, err := in.TransformRequest(context.Background(), &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, ContentType: "application/json", Body: []byte(`{"model":"image","prompt":"tree","stream":true}`)})
	if err == nil {
		t.Fatal("streaming image request must follow AxonHub's rejection")
	}
}
