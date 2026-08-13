package helper

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func newJSONRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "https://example.com/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	return request
}

func readBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	return body
}

func decodeBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(readBody(t, request), &decoded); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}
	return decoded
}

func ptr(s string) *string { return &s }

// 深合并：嵌套对象只覆盖命中的子键，其余子键保留。
func TestApplyParamOverrideDeepMergesNestedObject(t *testing.T) {
	request := newJSONRequest(t, `{"model":"claude","thinking":{"type":"enabled","budget_tokens":1024}}`)
	if err := ApplyParamOverride(request, ptr(`{"thinking":{"budget_tokens":4096}}`)); err != nil {
		t.Fatalf("ApplyParamOverride failed: %v", err)
	}

	decoded := decodeBody(t, request)
	thinking, ok := decoded["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking should stay an object, got %#v", decoded["thinking"])
	}
	if thinking["budget_tokens"] != float64(4096) {
		t.Fatalf("budget_tokens = %#v, want 4096", thinking["budget_tokens"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("type should be preserved by deep merge, got %#v", thinking["type"])
	}
	if decoded["model"] != "claude" {
		t.Fatalf("model should be untouched, got %#v", decoded["model"])
	}
}

// 数组整体替换：tools / messages 这类语义上不能逐元素合并。
func TestApplyParamOverrideReplacesArrays(t *testing.T) {
	request := newJSONRequest(t, `{"tools":[{"name":"a"},{"name":"b"}]}`)
	if err := ApplyParamOverride(request, ptr(`{"tools":[{"name":"c"}]}`)); err != nil {
		t.Fatalf("ApplyParamOverride failed: %v", err)
	}

	decoded := decodeBody(t, request)
	tools, ok := decoded["tools"].([]any)
	if !ok {
		t.Fatalf("tools should be an array, got %#v", decoded["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools should be replaced wholesale, got %d entries", len(tools))
	}
}

// 类型不一致时整体替换：对象 -> 标量，标量 -> 对象。
func TestApplyParamOverrideReplacesOnTypeMismatch(t *testing.T) {
	request := newJSONRequest(t, `{"thinking":{"type":"enabled"},"temperature":0.7}`)
	if err := ApplyParamOverride(request, ptr(`{"thinking":false,"temperature":{"mode":"auto"}}`)); err != nil {
		t.Fatalf("ApplyParamOverride failed: %v", err)
	}

	decoded := decodeBody(t, request)
	if decoded["thinking"] != false {
		t.Fatalf("thinking = %#v, want false", decoded["thinking"])
	}
	temperature, ok := decoded["temperature"].(map[string]any)
	if !ok || temperature["mode"] != "auto" {
		t.Fatalf("temperature = %#v, want {mode:auto}", decoded["temperature"])
	}
}

// 多层有序：后传入的层覆盖先传入的层，且深合并跨层生效。
func TestApplyParamOverridesLayerPriority(t *testing.T) {
	request := newJSONRequest(t, `{"temperature":0.1,"thinking":{"type":"enabled","budget_tokens":1}}`)
	err := ApplyParamOverrides(request,
		[]byte(`{"temperature":0.2,"top_p":0.5,"thinking":{"budget_tokens":2}}`), // 全局
		[]byte(`{"temperature":0.3,"thinking":{"budget_tokens":3}}`),             // 模型规则
		[]byte(`{"temperature":0.4}`),                                            // 渠道覆盖
	)
	if err != nil {
		t.Fatalf("ApplyParamOverrides failed: %v", err)
	}

	decoded := decodeBody(t, request)
	if decoded["temperature"] != float64(0.4) {
		t.Fatalf("temperature = %#v, want 0.4 (channel layer wins)", decoded["temperature"])
	}
	if decoded["top_p"] != float64(0.5) {
		t.Fatalf("top_p = %#v, want 0.5 (only global sets it)", decoded["top_p"])
	}
	thinking := decoded["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(3) {
		t.Fatalf("budget_tokens = %#v, want 3 (model rule layer wins)", thinking["budget_tokens"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("type should survive all layers, got %#v", thinking["type"])
	}
}

// 全空覆盖必须逐字节不动请求体：透传路径依赖字节保真（Anthropic prompt-cache）。
func TestApplyParamOverridesEmptyLayersKeepBodyBytes(t *testing.T) {
	original := `{"model":"claude","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`

	cases := []struct {
		name      string
		overrides [][]byte
	}{
		{"无覆盖", nil},
		{"空串", [][]byte{[]byte("")}},
		{"空白串", [][]byte{[]byte("   ")}},
		{"空对象", [][]byte{[]byte("{}")}},
		{"多个空层", [][]byte{[]byte("{}"), []byte(""), []byte("  ")}},
		{"畸形覆盖被跳过", [][]byte{[]byte(`{"temperature":`)}},
		{"数组覆盖被跳过", [][]byte{[]byte(`[1,2]`)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := newJSONRequest(t, original)
			contentLength := request.ContentLength
			if err := ApplyParamOverrides(request, tc.overrides...); err != nil {
				t.Fatalf("ApplyParamOverrides failed: %v", err)
			}
			got := readBody(t, request)
			if !bytes.Equal(got, []byte(original)) {
				t.Fatalf("body must stay byte-identical\n got: %s\nwant: %s", got, original)
			}
			if request.ContentLength != contentLength {
				t.Fatalf("ContentLength = %d, want %d", request.ContentLength, contentLength)
			}
		})
	}
}

// nil 覆盖串与 nil 请求都不应 panic。
func TestApplyParamOverrideNilInputs(t *testing.T) {
	request := newJSONRequest(t, `{"a":1}`)
	if err := ApplyParamOverride(request, nil); err != nil {
		t.Fatalf("nil override should be a no-op, got %v", err)
	}
	if got := readBody(t, request); string(got) != `{"a":1}` {
		t.Fatalf("body changed: %s", got)
	}
	if err := ApplyParamOverrides(nil, []byte(`{"a":2}`)); err != nil {
		t.Fatalf("nil request should be a no-op, got %v", err)
	}
	if err := ApplyParamOverrides(&http.Request{}, []byte(`{"a":2}`)); err != nil {
		t.Fatalf("nil body should be a no-op, got %v", err)
	}
}

// 非 JSON 对象请求体（multipart 等）原样恢复且不报错。
func TestApplyParamOverridesNonObjectBodyRestored(t *testing.T) {
	cases := []string{
		"--boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ngpt-image-1\r\n",
		`[1,2,3]`,
		`null`,
		`"just a string"`,
	}

	for _, body := range cases {
		request := newJSONRequest(t, body)
		if err := ApplyParamOverrides(request, []byte(`{"temperature":0.7}`)); err != nil {
			t.Fatalf("non-object body should not error, got %v", err)
		}
		if got := readBody(t, request); string(got) != body {
			t.Fatalf("body must be restored\n got: %s\nwant: %s", got, body)
		}
	}
}

// GetBody 必须重建，HTTP/2 与重试重放依赖它。
func TestApplyParamOverridesRebuildsGetBody(t *testing.T) {
	request := newJSONRequest(t, `{"temperature":0.1}`)
	if err := ApplyParamOverrides(request, []byte(`{"temperature":0.9}`)); err != nil {
		t.Fatalf("ApplyParamOverrides failed: %v", err)
	}
	if request.GetBody == nil {
		t.Fatal("GetBody must be set after override")
	}
	replay, err := request.GetBody()
	if err != nil {
		t.Fatalf("GetBody failed: %v", err)
	}
	replayed, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("failed to read replayed body: %v", err)
	}
	body := readBody(t, request)
	if !bytes.Equal(body, replayed) {
		t.Fatalf("GetBody replay mismatch\n body: %s\nreplay: %s", body, replayed)
	}
	if request.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d", request.ContentLength, len(body))
	}
}

// 同一覆盖重复应用结果一致：forwardViaHTTPStandard 在 item_reference 降级时会自我递归。
func TestApplyParamOverridesIdempotent(t *testing.T) {
	override := []byte(`{"temperature":0.9,"thinking":{"budget_tokens":4096}}`)

	request := newJSONRequest(t, `{"temperature":0.1,"thinking":{"type":"enabled"}}`)
	if err := ApplyParamOverrides(request, override); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	first := readBody(t, request)

	request.Body = io.NopCloser(bytes.NewReader(first))
	if err := ApplyParamOverrides(request, override); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	second := readBody(t, request)

	if !bytes.Equal(first, second) {
		t.Fatalf("apply must be idempotent\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestDeepMergeInto(t *testing.T) {
	cases := []struct {
		name string
		dst  map[string]any
		src  map[string]any
		want map[string]any
	}{
		{
			name: "新增键",
			dst:  map[string]any{"a": 1},
			src:  map[string]any{"b": 2},
			want: map[string]any{"a": 1, "b": 2},
		},
		{
			name: "标量覆盖",
			dst:  map[string]any{"a": 1},
			src:  map[string]any{"a": 2},
			want: map[string]any{"a": 2},
		},
		{
			name: "嵌套递归合并",
			dst:  map[string]any{"o": map[string]any{"x": 1, "y": 2}},
			src:  map[string]any{"o": map[string]any{"y": 3, "z": 4}},
			want: map[string]any{"o": map[string]any{"x": 1, "y": 3, "z": 4}},
		},
		{
			name: "多层嵌套",
			dst:  map[string]any{"a": map[string]any{"b": map[string]any{"c": 1, "d": 2}}},
			src:  map[string]any{"a": map[string]any{"b": map[string]any{"c": 9}}},
			want: map[string]any{"a": map[string]any{"b": map[string]any{"c": 9, "d": 2}}},
		},
		{
			name: "null 覆盖对象",
			dst:  map[string]any{"o": map[string]any{"x": 1}},
			src:  map[string]any{"o": nil},
			want: map[string]any{"o": nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deepMergeInto(tc.dst, tc.src)
			if !reflect.DeepEqual(tc.dst, tc.want) {
				t.Fatalf("deepMergeInto = %#v, want %#v", tc.dst, tc.want)
			}
		})
	}
}
