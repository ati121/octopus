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
