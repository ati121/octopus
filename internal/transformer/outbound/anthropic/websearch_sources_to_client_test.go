package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

// clientMessage 是 OpenAI Chat 客户端视角的 message 形状。
type clientMessage struct {
	Content     json.RawMessage `json:"content"`
	Annotations []struct {
		Type        string `json:"type"`
		URLCitation struct {
			URL       string `json:"url"`
			Title     string `json:"title"`
			CitedText string `json:"cited_text"`
		} `json:"url_citation"`
	} `json:"annotations"`
	SearchSources []struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		PageAge string `json:"page_age"`
	} `json:"search_sources"`
}

func decodeClientMessage(t *testing.T, body []byte) clientMessage {
	t.Helper()
	var probe struct {
		Choices []struct {
			Message clientMessage `json:"message"`
			Delta   clientMessage `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("client json unmarshal: %v (body=%s)", err, body)
	}
	if len(probe.Choices) == 0 {
		t.Fatalf("no choices in client response: %s", body)
	}
	if len(probe.Choices[0].Message.Annotations) > 0 || len(probe.Choices[0].Message.SearchSources) > 0 || len(probe.Choices[0].Message.Content) > 0 {
		return probe.Choices[0].Message
	}
	return probe.Choices[0].Delta
}

// 端到端：MiniMax 服务端执行搜索后返回 server_tool_use + web_search_tool_result +
// 带 citations 的 text，客户端必须拿到 annotations 与 search_sources，
// 且 content 里不残留服务端工具空壳。这正是 bug 报告的验收标准第 2 条。
func TestWebSearchSourcesReachOpenAIClient(t *testing.T) {
	upstream := []byte(`{
		"id":"msg_1","type":"message","role":"assistant","model":"MiniMax-M2.7",
		"content":[
			{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"魔兽世界时光服"}},
			{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[
				{"type":"web_search_result","url":"https://news.example.com/a","title":"时光服最新","page_age":"2 hours"},
				{"type":"web_search_result","url":"https://news.example.com/b","title":"官方公告"}
			]},
			{"type":"text","text":"根据搜索结果，2026 年……","citations":[
				{"type":"web_search_result_location","url":"https://news.example.com/a","title":"时光服最新","cited_text":"关键片段"}
			]}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":20}
	}`)

	var msg anthropicModel.Message
	if err := json.Unmarshal(upstream, &msg); err != nil {
		t.Fatalf("upstream parse: %v", err)
	}
	internal := convertToLLMResponse(&msg)

	in := &openaiInbound.ChatInbound{}
	body, err := in.TransformResponse(context.Background(), internal)
	if err != nil {
		t.Fatalf("inbound transform: %v", err)
	}
	got := decodeClientMessage(t, body)

	if len(got.Annotations) == 0 {
		t.Fatalf("annotations empty — caller would judge the answer sourceless: %s", body)
	}
	if got.Annotations[0].Type != "url_citation" {
		t.Fatalf("annotation type = %q, want url_citation", got.Annotations[0].Type)
	}
	if got.Annotations[0].URLCitation.URL != "https://news.example.com/a" {
		t.Fatalf("annotation url = %q", got.Annotations[0].URLCitation.URL)
	}
	if got.Annotations[0].URLCitation.CitedText != "关键片段" {
		t.Fatalf("cited_text lost: %q", got.Annotations[0].URLCitation.CitedText)
	}

	if len(got.SearchSources) != 2 {
		t.Fatalf("expected 2 search_sources, got %d: %s", len(got.SearchSources), body)
	}
	if got.SearchSources[0].PageAge != "2 hours" {
		t.Fatalf("page_age lost: %q", got.SearchSources[0].PageAge)
	}

	// content 必须折叠成纯文本字符串，不能残留 server_tool_* 空壳。
	if strings.Contains(string(body), "server_tool_use") || strings.Contains(string(body), "server_tool_result") {
		t.Fatalf("server tool husks leaked to the client: %s", body)
	}
	var text string
	if err := json.Unmarshal(got.Content, &text); err != nil {
		t.Fatalf("content should collapse to a plain string, got %s", got.Content)
	}
	if !strings.Contains(text, "2026") {
		t.Fatalf("answer text lost: %q", text)
	}
}

// 没有逐句 citations 时（模型只搜不标注），仍要凭结果块兜底出来源，
// 否则调用方照样判定无来源。
func TestWebSearchSourcesFallbackWithoutCitations(t *testing.T) {
	upstream := []byte(`{
		"id":"msg_2","type":"message","role":"assistant","model":"MiniMax-M2.7",
		"content":[
			{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[
				{"type":"web_search_result","url":"https://news.example.com/a","title":"A"}
			]},
			{"type":"text","text":"答案正文"}
		],
		"stop_reason":"end_turn"
	}`)

	var msg anthropicModel.Message
	if err := json.Unmarshal(upstream, &msg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	internal := convertToLLMResponse(&msg)
	in := &openaiInbound.ChatInbound{}
	body, err := in.TransformResponse(context.Background(), internal)
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	got := decodeClientMessage(t, body)
	if len(got.Annotations) != 1 || got.Annotations[0].URLCitation.URL != "https://news.example.com/a" {
		t.Fatalf("annotations not synthesized from results: %s", body)
	}
	if len(got.SearchSources) != 1 {
		t.Fatalf("search_sources missing: %s", body)
	}
}

// 无搜索的普通响应不得凭空长出这两个字段（omitempty 生效）。
func TestPlainResponseHasNoSourceFields(t *testing.T) {
	upstream := []byte(`{
		"id":"msg_3","type":"message","role":"assistant","model":"MiniMax-M2.7",
		"content":[{"type":"text","text":"普通回答"}],
		"stop_reason":"end_turn"
	}`)
	var msg anthropicModel.Message
	if err := json.Unmarshal(upstream, &msg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	internal := convertToLLMResponse(&msg)
	in := &openaiInbound.ChatInbound{}
	body, err := in.TransformResponse(context.Background(), internal)
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	for _, field := range []string{"annotations", "search_sources"} {
		if strings.Contains(string(body), field) {
			t.Fatalf("plain response should omit %q: %s", field, body)
		}
	}
}

// 流式：结果块随 content_block_start 一次到达，必须转成带来源的分片；
// 聚合后也要保留来源且不重复。
func TestWebSearchSourcesInStream(t *testing.T) {
	out := &MessageOutbound{}
	ctx := context.Background()

	events := [][]byte{
		[]byte(`{"type":"message_start","message":{"id":"msg_s","type":"message","role":"assistant","model":"MiniMax-M2.7","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`),
		[]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://news.example.com/a","title":"A","page_age":"1 day"}]}}`),
		[]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"答案"}}`),
		[]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://news.example.com/a","title":"A","cited_text":"片段"}}}`),
	}

	in := &openaiInbound.ChatInbound{}
	sawSourceChunk := false
	for _, ev := range events {
		chunk, err := out.TransformStream(ctx, ev)
		if err != nil {
			t.Fatalf("stream transform: %v", err)
		}
		if chunk == nil {
			continue
		}
		sse, err := in.TransformStream(ctx, chunk)
		if err != nil {
			t.Fatalf("inbound stream: %v", err)
		}
		if strings.Contains(string(sse), "url_citation") {
			sawSourceChunk = true
		}
		if strings.Contains(string(sse), "server_tool_result") {
			t.Fatalf("server tool husk leaked into a stream chunk: %s", sse)
		}
	}
	if !sawSourceChunk {
		t.Fatal("no stream chunk carried the search sources")
	}

	// 聚合结果里来源必须存在且按 URL 去重（结果块与 citations_delta 同一 URL）。
	final, err := in.GetInternalResponse(ctx)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if final == nil || len(final.Choices) == 0 || final.Choices[0].Message == nil {
		t.Fatalf("aggregated response empty: %#v", final)
	}
	agg := final.Choices[0].Message
	if len(agg.SearchSources) != 1 {
		t.Fatalf("expected 1 deduped source after aggregation, got %d: %#v", len(agg.SearchSources), agg.SearchSources)
	}
	if len(agg.Annotations) == 0 {
		t.Fatal("aggregated response lost annotations")
	}
}

// 空壳剔除不得误伤多模态等其它内容块。
func TestDropServerToolBlocksKeepsOtherParts(t *testing.T) {
	text := "hi"
	msg := &model.Message{
		Role: "assistant",
		Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{Type: "server_tool_use", ServerToolUse: &model.ServerToolUseBlock{ID: "s1"}},
			{Type: "text", Text: &text},
			{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://img.example.com/x.png"}},
		}},
	}
	msg.DropServerToolBlocks()
	if len(msg.Content.MultipleContent) != 2 {
		t.Fatalf("expected 2 surviving parts, got %#v", msg.Content.MultipleContent)
	}
	if msg.Content.MultipleContent[0].Type != "text" || msg.Content.MultipleContent[1].Type != "image_url" {
		t.Fatalf("wrong parts survived: %#v", msg.Content.MultipleContent)
	}
	if msg.Content.Content != nil {
		t.Fatal("multi-part content must not collapse to a string")
	}
}
