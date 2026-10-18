package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaToOpenAIResponseFormatRoundTrip(t *testing.T) {
	src := `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`
	s, err := ParseSchema([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := s.ToOpenAIResponseFormat()
	if err != nil {
		t.Fatalf("to openai: %v", err)
	}
	var reparsed map[string]any
	if err := json.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed["type"] != "object" {
		t.Fatalf("root not preserved: %v", reparsed)
	}
}

func TestResponseFormatUnmarshalWrapper(t *testing.T) {
	wire := `{
		"type":"json_schema",
		"json_schema":{
			"name":"User",
			"strict":true,
			"schema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}
		}
	}`
	var rf ResponseFormat
	if err := json.Unmarshal([]byte(wire), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Name != "User" || rf.Strict == nil || !*rf.Strict {
		t.Fatalf("wrapper fields lost: %+v", rf)
	}
	if rf.Schema == nil || rf.Schema.Type != "object" {
		t.Fatalf("schema not parsed: %+v", rf.Schema)
	}
	if len(rf.RawSchema) == 0 {
		t.Fatalf("raw schema not preserved")
	}
}

func TestResponseFormatUnmarshalBareSchema(t *testing.T) {
	wire := `{"type":"json_schema","json_schema":{"type":"object","properties":{"a":{"type":"string"}}}}`
	var rf ResponseFormat
	if err := json.Unmarshal([]byte(wire), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf.Schema == nil || rf.Schema.Type != "object" {
		t.Fatalf("bare schema not parsed: %+v", rf.Schema)
	}
	if rf.Name != "" || rf.Strict != nil {
		t.Fatalf("wrapper fields should be empty: %+v", rf)
	}
}

func TestResponseFormatMarshalReconstructsWrapper(t *testing.T) {
	strict := true
	rf := ResponseFormat{
		Type:   "json_schema",
		Name:   "Foo",
		Strict: &strict,
		Schema: &Schema{Type: "object", Required: []string{"x"}},
	}
	out, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"name":"Foo"`) {
		t.Errorf("marshal missing name: %s", out)
	}
	if !strings.Contains(string(out), `"strict":true`) {
		t.Errorf("marshal missing strict: %s", out)
	}
	if !strings.Contains(string(out), `"type":"object"`) {
		t.Errorf("marshal missing schema body: %s", out)
	}
}
