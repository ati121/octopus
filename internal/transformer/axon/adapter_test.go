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
	"github.com/looplj/axonhub/llm"
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

const decisionsAnswer = `{"model":"upstream","answers":[{"type":"choice","name":"department","choice":"billing"}],"usage":{"input_tokens":120}}`

// Decisions 的原始报文只在 llm.DecisionsRequest.Body（无 JSON tag）里，
// 必须经 DecisionsPayload 跨重试保留，否则出站请求体会为空。
func TestDecisionsRequestSurvivesNativeRoundTrip(t *testing.T) {
	ctx := t.Context()
	body := []byte(`{"model":"group","input":"hello","questions":[{"type":"choice","name":"department"}],"x_beta":1}`)
	in := NewInbound(6)
	request, err := in.TransformRequest(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	if request.RawAPIFormat != model.APIFormatOpenAIDecisions {
		t.Fatalf("unexpected api format %q", request.RawAPIFormat)
	}
	if len(request.DecisionsPayload) == 0 {
		t.Fatal("expected decisions payload projection")
	}

	request.Model = "upstream"
	out := NewOutbound(9)
	wire, err := out.TransformRequest(ctx, request, "https://gateway.example/v1", "upstream-key")
	if err != nil {
		t.Fatal(err)
	}
	if wire.URL.Path != "/v1/decisions" {
		t.Fatalf("path %s, want /v1/decisions", wire.URL.Path)
	}
	raw, _ := io.ReadAll(wire.Body)
	if !bytes.Contains(raw, []byte(`"input":"hello"`)) {
		t.Fatalf("lost decisions input: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"x_beta":1`)) {
		t.Fatalf("lost decisions extension fields: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"model":"upstream"`)) {
		t.Fatalf("expected routed model in payload: %s", raw)
	}

	response, err := out.TransformResponse(ctx, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(decisionsAnswer))})
	if err != nil {
		t.Fatal(err)
	}
	if !response.IsDecisionsResponse() {
		t.Fatal("expected decisions response projection")
	}
	rendered, err := in.TransformResponse(ctx, response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered, []byte(`"choice":"billing"`)) {
		t.Fatalf("expected upstream answers to be returned unchanged, got %s", rendered)
	}
}

// endpoint_path 让渠道把协议默认路径换掉：EMRank 这类上游把 System One
// 暴露在 /decisions，只能靠路径覆盖打通。
//
// AxonHub 在 EndpointPath 非空时不做 "/v1" 归一化，而是把路径原样拼在渠道
// BaseURL 之后。因此 base 已含 /v1 时，endpoint_path 只需写 /decisions。
func TestOutboundEndpointPathOverride(t *testing.T) {
	ctx := t.Context()
	systemOne := &model.InternalLLMRequest{
		Model: "upstream",
		Native: &llm.Request{
			Model:       "upstream",
			RequestType: llm.RequestTypeSystemOne,
			APIFormat:   llm.APIFormatTypeSafeSystemOne,
			SystemOne: &llm.SystemOneRequest{
				State:     "ping",
				Questions: map[string]llm.SystemOneQuestion{"probe": {Type: "noul", Instructions: "Is this a ping?"}},
			},
		},
	}

	cases := []struct {
		name     string
		kind     int
		base     string
		path     string
		wantPath string
	}{
		{name: "systemone default", kind: 8, base: "https://emrank.tech/v1", wantPath: "/v1/systemone"},
		{name: "systemone overridden", kind: 8, base: "https://emrank.tech/v1", path: "/decisions", wantPath: "/v1/decisions"},
		{name: "decisions default", kind: 9, base: "https://emrank.tech/v1", wantPath: "/v1/decisions"},
		{name: "decisions overridden", kind: 9, base: "https://emrank.tech/v1", path: "/systemone", wantPath: "/v1/systemone"},
		{name: "systemone overridden without versioned base", kind: 8, base: "https://emrank.tech", path: "/v1/decisions", wantPath: "/v1/decisions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := NewOutboundWithEndpointPath(tc.kind, tc.path)
			view := systemOne
			if tc.kind == 9 {
				view = &model.InternalLLMRequest{
					Model:            "upstream",
					DecisionsPayload: []byte(`{"model":"upstream","input":"ping","questions":[{"type":"noul","name":"probe","instructions":"Is this a ping?"}]}`),
				}
			}
			wire, err := out.TransformRequest(ctx, view, tc.base, "upstream-key")
			if err != nil {
				t.Fatal(err)
			}
			if wire.URL.Path != tc.wantPath {
				t.Fatalf("path %s, want %s", wire.URL.Path, tc.wantPath)
			}
		})
	}
}
