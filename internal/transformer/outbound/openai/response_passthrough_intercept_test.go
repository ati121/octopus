package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func mustIntercept(t *testing.T, proc model.PassthroughEventProcessor, raw string) [][]byte {
	t.Helper()
	synths, err := proc.Intercept([]byte(raw))
	if err != nil {
		t.Fatalf("Intercept(%s) error = %v", raw, err)
	}
	return synths
}

func unmarshalSynth(t *testing.T, payload []byte) ResponsesStreamEvent {
	t.Helper()
	var ev ResponsesStreamEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("synthesized payload %s is not valid JSON: %v", payload, err)
	}
	return ev
}

// opencode 风格上游：调用工具时只发 output_item.added + function_call_arguments.delta，
// 不发 function_call_arguments.done / output_item.done。透传路径必须在
// response.completed 前合成补齐，否则 Hermes 这类客户端会丢掉工具调用。
func TestPassthroughInterceptorSynthesizesMissingDoneForToolCall(t *testing.T) {
	proc := (&ResponseOutbound{}).NewPassthroughInterceptor()

	added := `{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`
	if synths := mustIntercept(t, proc, added); len(synths) != 0 {
		t.Fatalf("output_item.added must not synthesize, got %d payloads", len(synths))
	}

	delta1 := `{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":\""}`
	delta2 := `{"type":"response.function_call_arguments.delta","output_index":1,"delta":"beijing\"}"}`
	if synths := mustIntercept(t, proc, delta1); len(synths) != 0 {
		t.Fatalf("arguments delta must not synthesize, got %d payloads", len(synths))
	}
	if synths := mustIntercept(t, proc, delta2); len(synths) != 0 {
		t.Fatalf("arguments delta must not synthesize, got %d payloads", len(synths))
	}

	completed := `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`
	synths := mustIntercept(t, proc, completed)
	if len(synths) != 2 {
		t.Fatalf("expected 2 synthesized events before completed, got %d: %s", len(synths), synths)
	}

	argsDone := unmarshalSynth(t, synths[0])
	if argsDone.Type != "response.function_call_arguments.done" {
		t.Fatalf("first synthesized event must be function_call_arguments.done, got %s", argsDone.Type)
	}
	if argsDone.ItemID == nil || *argsDone.ItemID != "fc_1" {
		t.Fatalf("arguments.done item_id must match output_item.added, got %+v", argsDone.ItemID)
	}
	if argsDone.OutputIndex != 1 {
		t.Fatalf("arguments.done must carry output_index 1, got %d", argsDone.OutputIndex)
	}
	if argsDone.Arguments != `{"city":"beijing"}` {
		t.Fatalf("arguments.done must carry full accumulated arguments, got %q", argsDone.Arguments)
	}

	itemDone := unmarshalSynth(t, synths[1])
	if itemDone.Type != "response.output_item.done" {
		t.Fatalf("second synthesized event must be output_item.done, got %s", itemDone.Type)
	}
	if itemDone.OutputIndex != 1 {
		t.Fatalf("output_item.done must carry output_index 1, got %d", itemDone.OutputIndex)
	}
	item := itemDone.Item
	if item == nil || item.Type != "function_call" {
		t.Fatalf("output_item.done must carry function_call item, got %+v", item)
	}
	if item.ID != "fc_1" || item.CallID != "call_1" || item.Name != "get_weather" {
		t.Fatalf("output_item.done item must keep added identity, got %+v", item)
	}
	if item.Arguments != `{"city":"beijing"}` {
		t.Fatalf("output_item.done item must carry full arguments, got %q", item.Arguments)
	}
	if item.Status == nil || *item.Status != "completed" {
		t.Fatalf("output_item.done item must carry status=completed, got %+v", item.Status)
	}

	// 幂等：重复终态不再合成。
	if synths := mustIntercept(t, proc, completed); len(synths) != 0 {
		t.Fatalf("second terminal must not re-synthesize, got %d payloads", len(synths))
	}
}

// 完整上游（基元律动风格）已经发了 done 事件，拦截器不得重复合成。
func TestPassthroughInterceptorSkipsWhenUpstreamSendsDone(t *testing.T) {
	proc := (&ResponseOutbound{}).NewPassthroughInterceptor()

	for _, raw := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"a\":1}"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{\"a\":1}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"a\":1}","status":"completed"}}`,
	} {
		if synths := mustIntercept(t, proc, raw); len(synths) != 0 {
			t.Fatalf("event %s must not synthesize, got %d payloads", raw, synths)
		}
	}

	completed := `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`
	if synths := mustIntercept(t, proc, completed); len(synths) != 0 {
		t.Fatalf("complete upstream must not synthesize before terminal, got %d payloads", len(synths))
	}
}

// 并行工具调用：message 在 output_index 0、两个 function_call 在 1 和 2。
// 只合成 function_call 的 done，且按 output_index 升序保持顺序稳定。
func TestPassthroughInterceptorParallelCallsSynthesizeOnlyFunctionCalls(t *testing.T) {
	proc := (&ResponseOutbound{}).NewPassthroughInterceptor()

	for _, raw := range []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"checking"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":\"beijing\"}"}`,
		`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"get_time"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"tz\":\"asia\"}"}`,
	} {
		if synths := mustIntercept(t, proc, raw); len(synths) != 0 {
			t.Fatalf("event must not synthesize, got %d payloads", len(synths))
		}
	}

	completed := `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`
	synths := mustIntercept(t, proc, completed)
	if len(synths) != 4 {
		t.Fatalf("expected 4 synthesized events (2 calls × args+item), got %d: %s", len(synths), synths)
	}

	// 顺序：index 1 的两个 done，再 index 2 的两个 done。
	wantOrder := []struct {
		typ   string
		index int
		args  string
	}{
		{"response.function_call_arguments.done", 1, `{"city":"beijing"}`},
		{"response.output_item.done", 1, `{"city":"beijing"}`},
		{"response.function_call_arguments.done", 2, `{"tz":"asia"}`},
		{"response.output_item.done", 2, `{"tz":"asia"}`},
	}
	for i, want := range wantOrder {
		ev := unmarshalSynth(t, synths[i])
		if ev.Type != want.typ || ev.OutputIndex != want.index {
			t.Fatalf("synth[%d] = (%s, index %d), want (%s, index %d)", i, ev.Type, ev.OutputIndex, want.typ, want.index)
		}
		if ev.Type == "response.function_call_arguments.done" && ev.Arguments != want.args {
			t.Fatalf("synth[%d] arguments = %q, want %q", i, ev.Arguments, want.args)
		}
	}
}

// 纯文本会话与杂项帧（[DONE]、非 JSON、注释）一律不合成。
func TestPassthroughInterceptorTextOnlyAndMiscNeverSynthesize(t *testing.T) {
	proc := (&ResponseOutbound{}).NewPassthroughInterceptor()

	for _, raw := range []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hello"}`,
		`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"think"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
		"[DONE]",
		"",
		"this is not json",
	} {
		if synths := mustIntercept(t, proc, raw); len(synths) != 0 {
			t.Fatalf("event %q must not synthesize, got %d payloads", raw, len(synths))
		}
	}
}

// 部分上游不发 output_item.added，只有 arguments delta：仍要能合成，
// 缺失的 item id 由网关生成补齐。
func TestPassthroughInterceptorDeltaWithoutAddedStillSynthesizes(t *testing.T) {
	proc := (&ResponseOutbound{}).NewPassthroughInterceptor()

	for _, raw := range []string{
		`{"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_1","name":"get_weather","delta":"{\"city\":\"beijing\"}"}`,
	} {
		if synths := mustIntercept(t, proc, raw); len(synths) != 0 {
			t.Fatalf("event must not synthesize, got %d payloads", len(synths))
		}
	}

	completed := `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`
	synths := mustIntercept(t, proc, completed)
	if len(synths) != 2 {
		t.Fatalf("expected 2 synthesized events, got %d: %s", len(synths), synths)
	}
	argsDone := unmarshalSynth(t, synths[0])
	if argsDone.ItemID == nil || *argsDone.ItemID == "" {
		t.Fatalf("expected generated item_id for missing added event, got %+v", argsDone.ItemID)
	}
	if argsDone.Arguments != `{"city":"beijing"}` {
		t.Fatalf("arguments must accumulate from deltas, got %q", argsDone.Arguments)
	}
	itemDone := unmarshalSynth(t, synths[1])
	if itemDone.Item == nil || itemDone.Item.ID == "" || itemDone.Item.CallID != "call_1" || itemDone.Item.Name != "get_weather" {
		t.Fatalf("output_item.done must carry identity from deltas, got %+v", itemDone.Item)
	}
}

// 链路验证（chat 协议请求 response 上游）：opencode 风格流（无任何 done 事件）
// 走标准 IR 转换链时，必须产出工具调用与 finish_reason=tool_calls，
// 证明 Chat 客户端在缺失 done 事件的上游下也能继续工具执行。
func TestOpenCodeStyleStreamYieldsToolCallsInStandardChain(t *testing.T) {
	o := &ResponseOutbound{}
	raws := []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"deepseek-v4-flash"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":\""}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"beijing\"}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","model":"deepseek-v4-flash","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
	}

	var all []model.StreamEvent
	for _, raw := range raws {
		events, err := o.TransformStreamEvent(context.Background(), []byte(raw))
		if err != nil {
			t.Fatalf("TransformStreamEvent(%s) error = %v", raw, err)
		}
		all = append(all, events...)
	}

	resp := model.InternalResponseFromStreamEvents(all)
	if resp == nil {
		t.Fatalf("aggregate must not be nil")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected single choice, got %+v", resp.Choices)
	}
	choice := resp.Choices[0]
	if choice.FinishReason == nil || *choice.FinishReason != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls for tool-calling upstream, got %+v", choice.FinishReason)
	}
	toolCalls := choice.Delta.ToolCalls
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", toolCalls)
	}
	if toolCalls[0].ID != "call_1" || toolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool call identity lost, got %+v", toolCalls[0])
	}
	if toolCalls[0].Function.Arguments != `{"city":"beijing"}` {
		t.Fatalf("tool call arguments not accumulated, got %q", toolCalls[0].Function.Arguments)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 {
		t.Fatalf("expected usage captured from completed event, got %+v", resp.Usage)
	}
}
