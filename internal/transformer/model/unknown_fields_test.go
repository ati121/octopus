package model

import (
	"encoding/json"
	"testing"
)

func TestCaptureUnknownRequestFields(t *testing.T) {
	body := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hi"}],
		"temperature": 0.7,
		"some_new_param": {"a": 1},
		"another_new_flag": true
	}`)

	var req InternalLLMRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	CaptureUnknownRequestFields(&req, body)

	if len(req.UnknownFields) != 2 {
		t.Fatalf("expected 2 unknown fields, got %d: %v", len(req.UnknownFields), req.UnknownFields)
	}
	if _, ok := req.UnknownFields["some_new_param"]; !ok {
		t.Fatal("some_new_param not captured")
	}
	if _, ok := req.UnknownFields["another_new_flag"]; !ok {
		t.Fatal("another_new_flag not captured")
	}
	// Known fields must NOT be captured.
	for _, known := range []string{"model", "messages", "temperature"} {
		if _, ok := req.UnknownFields[known]; ok {
			t.Fatalf("known field %q wrongly captured as unknown", known)
		}
	}
}

func TestCaptureUnknownRequestFieldsAllKnown(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"top_p":0.9}`)
	var req InternalLLMRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	CaptureUnknownRequestFields(&req, body)
	if req.UnknownFields != nil {
		t.Fatalf("expected nil UnknownFields, got %v", req.UnknownFields)
	}
}

func TestCaptureUnknownRequestFieldsInvalidJSON(t *testing.T) {
	var req InternalLLMRequest
	// Must not panic on non-object / invalid input.
	CaptureUnknownRequestFields(&req, []byte("not json"))
	CaptureUnknownRequestFields(&req, []byte("[1,2,3]"))
	CaptureUnknownRequestFields(nil, []byte(`{"x":1}`))
	if req.UnknownFields != nil {
		t.Fatalf("expected nil UnknownFields, got %v", req.UnknownFields)
	}
}

func TestMergeUnknownFields(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","temperature":0.7}`)
	unknown := map[string]json.RawMessage{
		"some_new_param": json.RawMessage(`{"a":1}`),
	}
	merged := MergeUnknownFields(body, unknown)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(merged, &obj); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if _, ok := obj["some_new_param"]; !ok {
		t.Fatal("some_new_param not merged")
	}
	if _, ok := obj["model"]; !ok {
		t.Fatal("model lost during merge")
	}
}

func TestMergeUnknownFieldsDoesNotOverwrite(t *testing.T) {
	// outbound already wrote "temperature"; unknown carrying the same key must
	// NOT clobber the outbound-normalized value.
	body := []byte(`{"model":"gpt-4o","temperature":0.2}`)
	unknown := map[string]json.RawMessage{
		"temperature": json.RawMessage(`0.9`),
	}
	merged := MergeUnknownFields(body, unknown)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(merged, &obj); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if string(obj["temperature"]) != "0.2" {
		t.Fatalf("temperature was overwritten: got %s, want 0.2", obj["temperature"])
	}
}

func TestMergeUnknownFieldsEmpty(t *testing.T) {
	body := []byte(`{"model":"gpt-4o"}`)
	if got := MergeUnknownFields(body, nil); string(got) != string(body) {
		t.Fatalf("nil unknown should return body unchanged, got %s", got)
	}
	if got := MergeUnknownFields(body, map[string]json.RawMessage{}); string(got) != string(body) {
		t.Fatalf("empty unknown should return body unchanged, got %s", got)
	}
}

// 驼峰写法的已建模字段按正名收编，不再原样转发给上游。
// 现实来源：客户端把 TS 侧选项名直接塞进请求体，基元律动对 promptCacheKey
// 返回 400 UNKNOWN_FIELD。
func TestCaptureUnknownRequestFieldsNormalizesCamelCaseAlias(t *testing.T) {
	body := []byte(`{
		"model": "deepseek-v4-flash",
		"promptCacheKey": "ses_9wOAbLLKfff",
		"maxCompletionTokens": 4096,
		"still_unknown": 1
	}`)

	var req InternalLLMRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	CaptureUnknownRequestFields(&req, body)

	if req.PromptCacheKey == nil || *req.PromptCacheKey != "ses_9wOAbLLKfff" {
		t.Fatalf("promptCacheKey not adopted as prompt_cache_key: %v", req.PromptCacheKey)
	}
	if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 4096 {
		t.Fatalf("maxCompletionTokens not adopted: %v", req.MaxCompletionTokens)
	}
	for _, alias := range []string{"promptCacheKey", "maxCompletionTokens"} {
		if _, ok := req.UnknownFields[alias]; ok {
			t.Fatalf("alias %q must not be forwarded verbatim", alias)
		}
	}
	// 真正未建模的字段仍然保全（F-1 的前向兼容初衷不变）。
	if _, ok := req.UnknownFields["still_unknown"]; !ok {
		t.Fatalf("genuinely unknown field was dropped: %v", req.UnknownFields)
	}
}

// 客户端同时发了正名和驼峰别名时，以正名为准，别名直接丢弃而非转发。
func TestCaptureUnknownRequestFieldsPrefersCanonicalOverAlias(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","prompt_cache_key":"canonical","promptCacheKey":"alias"}`)

	var req InternalLLMRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	CaptureUnknownRequestFields(&req, body)

	if req.PromptCacheKey == nil || *req.PromptCacheKey != "canonical" {
		t.Fatalf("canonical value was clobbered: %v", req.PromptCacheKey)
	}
	if _, ok := req.UnknownFields["promptCacheKey"]; ok {
		t.Fatal("duplicate alias must not be forwarded")
	}
}

// 别名值类型不匹配时安静丢弃：字段保持零值，也不回退成原样转发。
func TestCaptureUnknownRequestFieldsIgnoresMistypedAlias(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","promptCacheKey":123,"topP":0.9}`)

	var req InternalLLMRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	CaptureUnknownRequestFields(&req, body)

	if req.PromptCacheKey != nil {
		t.Fatalf("mistyped alias should be dropped, got %v", *req.PromptCacheKey)
	}
	if _, ok := req.UnknownFields["promptCacheKey"]; ok {
		t.Fatal("mistyped alias must not be forwarded")
	}
	// 同批次里类型正确的别名不受连累。
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Fatalf("topP not adopted: %v", req.TopP)
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := map[string]string{
		"promptCacheKey":      "prompt_cache_key",
		"maxCompletionTokens": "max_completion_tokens",
		"topP":                "top_p",
		"prompt_cache_key":    "prompt_cache_key",
		"model":               "model",
		"":                    "",
	}
	for in, want := range cases {
		if got := toSnakeCase(in); got != want {
			t.Fatalf("toSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
