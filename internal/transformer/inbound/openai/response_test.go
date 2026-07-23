package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertToInternalRequestPreservesRawInputItems(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{
			{Type: "input_text", Text: stringPtr("hello")},
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if len(internalReq.RawInputItems) == 0 {
		t.Fatalf("expected raw input items to be preserved")
	}

	var items []map[string]any
	if err := json.Unmarshal(internalReq.RawInputItems, &items); err != nil {
		t.Fatalf("unmarshal raw input items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "input_text" {
		t.Fatalf("expected original raw input items to be kept, got %#v", items)
	}
	if internalReq.TransformOptions.ArrayInputs == nil || !*internalReq.TransformOptions.ArrayInputs {
		t.Fatalf("expected array input flag to stay true")
	}
}

func TestConvertInputToMessagesMergesParallelFunctionCallsAndMapsOutputs(t *testing.T) {
	outputA := ResponsesInput{Text: stringPtr("result-a")}
	outputB := ResponsesInput{Text: stringPtr("result-b")}
	itemReference := "fc_item_b"
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("find both")}},
		{ID: "fc_item_a", Type: "function_call", CallID: "call_a", Name: "search_files", Arguments: `{"q":"a"}`},
		{ID: "fc_item_b", Type: "function_call", CallID: "call_b", Name: "terminal", Arguments: `{"cmd":"pwd"}`},
		{Type: "function_call_output", CallID: "call_a", Output: &outputA},
		{Type: "function_call_output", ItemReference: &itemReference, Output: &outputB},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected user, one assistant tool message, and two tool outputs, got %#v", messages)
	}
	if messages[0].Role != "user" || messages[0].Content.Content == nil || *messages[0].Content.Content != "find both" {
		t.Fatalf("unexpected user message: %#v", messages[0])
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 2 {
		t.Fatalf("expected parallel calls in one assistant message, got %#v", messages[1])
	}
	if messages[1].ToolCalls[0].ID != "call_a" || messages[1].ToolCalls[0].Function.Name != "search_files" || messages[1].ToolCalls[1].ID != "call_b" || messages[1].ToolCalls[1].Function.Name != "terminal" {
		t.Fatalf("parallel tool call order or ids changed: %#v", messages[1].ToolCalls)
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID == nil || *messages[2].ToolCallID != "call_a" {
		t.Fatalf("unexpected first tool output: %#v", messages[2])
	}
	if messages[3].Role != "tool" || messages[3].ToolCallID == nil || *messages[3].ToolCallID != "call_b" {
		t.Fatalf("item_reference was not mapped to call_b: %#v", messages[3])
	}
}

func TestResponseInboundTransformRequestMergesParallelToolCalls(t *testing.T) {
	body := []byte(`{
		"model":"MiniMax-M2.7",
		"parallel_tool_calls":true,
		"input":[
				{"id":"fc_a","type":"function_call","call_id":"call_a","name":"search_files","arguments":{"q":"a"}},
			{"id":"fc_b","type":"function_call","call_id":"call_b","name":"terminal","arguments":"{\"cmd\":\"pwd\"}"},
			{"type":"function_call_output","call_id":"call_a","output":"result-a"},
			{"type":"function_call_output","call_id":"call_b","output":"result-b"}
		]
	}`)

	req, err := (&ResponseInbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls was not preserved: %#v", req.ParallelToolCalls)
	}
	if len(req.Messages) != 3 || req.Messages[0].Role != "assistant" || len(req.Messages[0].ToolCalls) != 2 || req.Messages[1].Role != "tool" || req.Messages[2].Role != "tool" {
		t.Fatalf("unexpected transformed protocol order: %#v", req.Messages)
	}
	if req.Messages[0].ToolCalls[0].Function.Arguments != `{"q":"a"}` {
		t.Fatalf("object arguments were not preserved as JSON: %q", req.Messages[0].ToolCalls[0].Function.Arguments)
	}
}

func TestConvertInputToMessagesKeepsSingleToolCallAndNormalMessages(t *testing.T) {
	output := ResponsesInput{Text: stringPtr("done")}
	input := &ResponsesInput{Items: []ResponsesItem{
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("hello")}},
		{ID: "fc_item", Type: "function_call", CallID: "call_1", Name: "lookup", Arguments: `{}`},
		{Type: "function_call_output", CallID: "call_1", Output: &output},
		{Type: "message", Role: "user", Content: &ResponsesInput{Text: stringPtr("continue")}},
	}}

	messages, err := convertInputToMessages(input)
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 4 || messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 || messages[2].Role != "tool" || messages[3].Role != "user" {
		t.Fatalf("single tool call or normal message order changed: %#v", messages)
	}
	if messages[1].ToolCalls[0].ID != "call_1" || messages[2].ToolCallID == nil || *messages[2].ToolCallID != "call_1" {
		t.Fatalf("single tool call id was not preserved: %#v", messages)
	}
}

func TestConvertFunctionCallOutputWithoutOutputDoesNotPanic(t *testing.T) {
	msg, err := convertItemToMessage(&ResponsesItem{Type: "function_call_output", CallID: "call_empty"})
	if err != nil {
		t.Fatalf("convertItemToMessage failed: %v", err)
	}
	if msg == nil || msg.Role != "tool" || msg.ToolCallID == nil || *msg.ToolCallID != "call_empty" {
		t.Fatalf("unexpected empty tool output message: %#v", msg)
	}
	if msg.Content.Content == nil || *msg.Content.Content != "" {
		t.Fatalf("expected empty content for missing output, got %#v", msg.Content)
	}
}

func TestConvertInputToMessagesUsesInternalToolCallShape(t *testing.T) {
	messages, err := convertInputToMessages(&ResponsesInput{Items: []ResponsesItem{
		{ID: "fc_id", Type: "function_call", Name: "lookup", Arguments: `{}`},
	}})
	if err != nil {
		t.Fatalf("convertInputToMessages failed: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "assistant" || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("expected one assistant tool message, got %#v", messages)
	}
	if messages[0].ToolCalls[0].Type != "function" || messages[0].ToolCalls[0].ID != "fc_id" {
		t.Fatalf("expected function call id fallback to item id, got %#v", messages[0].ToolCalls[0])
	}
}

func TestFlexibleJSONStringAcceptsStringObjectAndNull(t *testing.T) {
	var value FlexibleJSONString
	if err := json.Unmarshal([]byte(`"hello"`), &value); err != nil || value.String() != "hello" {
		t.Fatalf("string: err=%v value=%q", err, value)
	}
	if err := json.Unmarshal([]byte(`{ "query": "test" }`), &value); err != nil || !strings.Contains(value.String(), `"query"`) {
		t.Fatalf("object: err=%v value=%q", err, value)
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || raw[0] != '{' {
		t.Fatalf("object representation was not preserved: raw=%s err=%v", raw, err)
	}
	standard := FlexibleJSONString(`{"query":"test"}`)
	raw, err = json.Marshal(standard)
	if err != nil || string(raw) != `"{\"query\":\"test\"}"` {
		t.Fatalf("internally-created function arguments must remain a JSON string: raw=%s err=%v", raw, err)
	}
	if err := json.Unmarshal([]byte(`null`), &value); err != nil || value.String() != "" {
		t.Fatalf("null: err=%v value=%q", err, value)
	}
}

func TestResponsesInputAcceptsNull(t *testing.T) {
	var input ResponsesInput
	if err := json.Unmarshal([]byte(`null`), &input); err != nil {
		t.Fatalf("null content: %v", err)
	}
	if input.Text != nil || len(input.Items) != 0 {
		t.Fatalf("expected empty input, got %#v", input)
	}
}

func TestTransformRequestPreservesObjectArgumentsForResponsesPassthrough(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"reasoning","content":null,"summary":[]},
			{"type":"tool_search_call","call_id":"call_search","arguments":{"query":"review","limit":5}}
		]
	}`)
	req, err := (&ResponseInbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	if !req.HasOpenAIResponsesPassthrough() {
		t.Fatal("expected unsupported tool_search_call to require passthrough")
	}
	var items []map[string]any
	if err := json.Unmarshal(req.OpenAIRawInputItems(), &items); err != nil {
		t.Fatalf("raw input items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("raw items: %#v", items)
	}
	arguments, ok := items[1]["arguments"].(map[string]any)
	if !ok || arguments["query"] != "review" {
		t.Fatalf("object arguments were not preserved: %#v", items[1]["arguments"])
	}
}

func TestConvertToInternalRequestMarksPassthroughForUnsupportedToolType(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Text: stringPtr("hello")},
		Tools: []ResponsesTool{{
			Type: "apply_patch",
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if !internalReq.HasOpenAIResponsesPassthrough() {
		t.Fatalf("expected unsupported responses tool to require passthrough")
	}
	if ext := internalReq.GetOpenAIExtensions(); !ext.ResponsesPassthroughRequired || ext.ResponsesPassthroughReason != "tool:apply_patch" {
		t.Fatalf("expected OpenAI extension passthrough view, got %#v", ext)
	}
}

func TestConvertToInternalRequestMarksPassthroughForUnsupportedInputItem(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{{
			Type:   "apply_patch_call_output",
			CallID: "apc_123",
		}}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if !internalReq.HasOpenAIResponsesPassthrough() {
		t.Fatalf("expected unsupported responses input item to require passthrough")
	}
	if ext := internalReq.GetOpenAIExtensions(); !ext.ResponsesPassthroughRequired || ext.ResponsesPassthroughReason != "input:apply_patch_call_output" {
		t.Fatalf("expected OpenAI extension passthrough view, got %#v", ext)
	}
}

func TestConvertToInternalRequestDoesNotMarkPassthroughForSupportedFileAndAudioInputs(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{
			{
				Type: "message",
				Role: "user",
				Content: &ResponsesInput{Items: []ResponsesItem{
					{Type: "input_file", FileID: stringPtr("file_123")},
					{Type: "input_audio", InputAudio: &ResponsesInputAudio{Format: "wav", Data: "AAA="}},
				}},
			},
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if internalReq.HasOpenAIResponsesPassthrough() {
		t.Fatalf("expected supported file/audio inputs to stay normalized without passthrough")
	}
	if len(internalReq.Messages) != 1 || len(internalReq.Messages[0].Content.MultipleContent) != 2 {
		t.Fatalf("expected supported file/audio inputs to normalize into message content, got %#v", internalReq.Messages)
	}
	if internalReq.Messages[0].Content.MultipleContent[0].Type != "file" {
		t.Fatalf("expected file content part, got %#v", internalReq.Messages[0].Content.MultipleContent[0])
	}
	if internalReq.Messages[0].Content.MultipleContent[1].Type != "input_audio" {
		t.Fatalf("expected input_audio content part, got %#v", internalReq.Messages[0].Content.MultipleContent[1])
	}
}

func TestConvertToInternalRequestNormalizesTopLevelInputFile(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{{
			Type:     "input_file",
			FileID:   stringPtr("file_456"),
			Filename: stringPtr("notes.txt"),
		}}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if internalReq.HasOpenAIResponsesPassthrough() {
		t.Fatalf("expected top-level input_file to stay normalized without passthrough")
	}
	if len(internalReq.Messages) != 1 {
		t.Fatalf("expected one normalized message, got %#v", internalReq.Messages)
	}
	if internalReq.Messages[0].Role != "user" {
		t.Fatalf("expected top-level input_file to default to user role, got %#v", internalReq.Messages[0].Role)
	}
	if len(internalReq.Messages[0].Content.MultipleContent) != 1 || internalReq.Messages[0].Content.MultipleContent[0].Type != "file" {
		t.Fatalf("expected top-level input_file to become file content, got %#v", internalReq.Messages[0].Content)
	}
	if internalReq.Messages[0].Content.MultipleContent[0].File == nil || internalReq.Messages[0].Content.MultipleContent[0].File.FileID != "file_456" {
		t.Fatalf("expected normalized file reference to preserve file_id, got %#v", internalReq.Messages[0].Content.MultipleContent[0].File)
	}
}

func stringPtr(value string) *string {
	return &value
}
