package anthropic

import (
	"encoding/json"
	"testing"
)

// web_search 服务端工具会让 Anthropic 在 text 块上回传 `citations` 数组
// （每项是一处引用位置）。而请求侧 document 块的 `citations` 是
// `{"enabled":true}` 对象。两种形态共用一个字段名，解析必须都能吃下，
// 否则带引用来源的整条响应会解析失败，客户端收不到任何来源。
func TestCitationsArrayOnTextBlock(t *testing.T) {
	raw := []byte(`{
		"type": "text",
		"text": "魔兽世界时光服最新消息……",
		"citations": [
			{"type":"web_search_result_location","url":"https://a.com/1","title":"A","cited_text":"片段一"},
			{"type":"web_search_result_location","url":"https://b.com/2","title":"B","cited_text":"片段二"}
		]
	}`)

	var block MessageContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal text block with citations array: %v", err)
	}
	if block.Citations == nil {
		t.Fatal("citations array was dropped")
	}
	if !block.Citations.Enabled {
		t.Fatal("non-empty citations array should mark citations as enabled")
	}
	if len(block.Citations.Locations) == 0 {
		t.Fatal("citation locations raw payload was not preserved")
	}

	// 原文必须可以按数组重新解析出来，保证透传时不丢来源。
	var locations []map[string]any
	if err := json.Unmarshal(block.Citations.Locations, &locations); err != nil {
		t.Fatalf("preserved locations are not a valid array: %v", err)
	}
	if len(locations) != 2 {
		t.Fatalf("expected 2 citation locations, got %d", len(locations))
	}
	if locations[0]["url"] != "https://a.com/1" {
		t.Fatalf("first citation url lost: %v", locations[0]["url"])
	}
}

// 请求侧的对象形态保持原有语义。
func TestCitationsObjectOnDocumentBlock(t *testing.T) {
	raw := []byte(`{"type":"document","citations":{"enabled":true}}`)

	var block MessageContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal document block with citations object: %v", err)
	}
	if block.Citations == nil || !block.Citations.Enabled {
		t.Fatalf("citations.enabled not parsed: %#v", block.Citations)
	}
	if len(block.Citations.Locations) != 0 {
		t.Fatal("object form must not populate Locations")
	}
}

// 空数组不应被当成「带引用」。
func TestCitationsEmptyArray(t *testing.T) {
	var block MessageContentBlock
	if err := json.Unmarshal([]byte(`{"type":"text","text":"x","citations":[]}`), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Citations == nil {
		t.Fatal("expected citations struct to be present")
	}
	if block.Citations.Enabled {
		t.Fatal("empty citations array must not mark enabled")
	}
}

// 两种形态都要能按来源原样序列化回去。
func TestCitationsMarshalRoundTrip(t *testing.T) {
	t.Run("array form replays raw payload", func(t *testing.T) {
		in := []byte(`{"type":"text","text":"x","citations":[{"type":"web_search_result_location","url":"https://a.com"}]}`)
		var block MessageContentBlock
		if err := json.Unmarshal(in, &block); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var reparsed MessageContentBlock
		if err := json.Unmarshal(out, &reparsed); err != nil {
			t.Fatalf("re-unmarshal produced invalid shape: %v (body=%s)", err, out)
		}
		if reparsed.Citations == nil || len(reparsed.Citations.Locations) == 0 {
			t.Fatalf("citations lost across round-trip: %s", out)
		}
	})

	t.Run("object form stays an object", func(t *testing.T) {
		block := MessageContentBlock{
			Type:      "document",
			Citations: &DocumentCitationsControl{Enabled: true},
		}
		out, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var probe struct {
			Citations struct {
				Enabled bool `json:"enabled"`
			} `json:"citations"`
		}
		if err := json.Unmarshal(out, &probe); err != nil {
			t.Fatalf("object form did not serialize as an object: %v (body=%s)", err, out)
		}
		if !probe.Citations.Enabled {
			t.Fatalf("enabled flag lost: %s", out)
		}
	})
}

// 完整的 web_search 响应（server_tool_use + web_search_tool_result + 带引用的
// text）必须整体解析成功。这是本次 bug 的端到端形态。
func TestWebSearchResponseWithCitationsParses(t *testing.T) {
	raw := []byte(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "MiniMax-M2.7",
		"content": [
			{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"魔兽世界时光服"}},
			{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[
				{"type":"web_search_result","url":"https://a.com/1","title":"A","page_age":"1 day"}
			]},
			{"type":"text","text":"根据搜索结果……","citations":[
				{"type":"web_search_result_location","url":"https://a.com/1","title":"A","cited_text":"片段"}
			]}
		],
		"stop_reason": "end_turn"
	}`)

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("full web_search response failed to parse: %v", err)
	}
	if len(msg.Content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(msg.Content))
	}
	if msg.Content[2].Citations == nil || len(msg.Content[2].Citations.Locations) == 0 {
		t.Fatal("citations on the text block were lost")
	}
}
