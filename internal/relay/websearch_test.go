package relay

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

func TestIsWebSearchToolName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"web_search", true},
		{"WebSearch", true},
		{"web_search_preview", true},
		{"builtin_web_search", true},
		{"web_search_20250305", true},
		{"web_search_20260318", true},
		{"x_search", true},
		{"x_keyword_search", true},
		{"x_semantic_search", true},
		{"get_weather", false},
		{"web_fetch", false},
		{"", false},
		{"code_execution", false},
	}
	for _, tc := range cases {
		if got := isWebSearchToolName(tc.name); got != tc.want {
			t.Errorf("isWebSearchToolName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsWebSearchToolType(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"web_search", true},
		{"web_search_20250305", true},
		{"function", false},
		{"computer_20250124", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isWebSearchToolType(tc.name); got != tc.want {
			t.Errorf("isWebSearchToolType(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHasWebSearchTool(t *testing.T) {
	req := &model.InternalLLMRequest{
		Tools: []model.Tool{
			{Type: "function", Function: model.Function{Name: "web_search"}},
			{Type: "function", Function: model.Function{Name: "get_weather"}},
		},
	}
	if !hasWebSearchTool(req) {
		t.Fatal("expected hasWebSearchTool to detect function web_search tool")
	}

	req2 := &model.InternalLLMRequest{
		Tools: []model.Tool{
			{Type: "function", Function: model.Function{Name: "get_weather"}},
		},
	}
	if hasWebSearchTool(req2) {
		t.Fatal("expected hasWebSearchTool to be false for non-search tools")
	}

	if hasWebSearchTool(nil) {
		t.Fatal("expected hasWebSearchTool(nil) to be false")
	}
}

func TestHasGatewayManagedWebSearchToolDistinguishesFunctionTools(t *testing.T) {
	ordinaryFunction := &model.InternalLLMRequest{
		Tools: []model.Tool{{
			Type:     "function",
			Function: model.Function{Name: "web_search"},
		}},
	}
	if hasGatewayManagedWebSearchTool(ordinaryFunction) {
		t.Fatal("ordinary function named web_search must not enable gateway buffering")
	}

	nativeType := &model.InternalLLMRequest{
		Tools: []model.Tool{{Type: "web_search"}},
	}
	if !hasGatewayManagedWebSearchTool(nativeType) {
		t.Fatal("provider-native web_search type must enable gateway management")
	}

	anthropicNative := &model.InternalLLMRequest{
		Tools: []model.Tool{{
			Type:                "web_search_20250305",
			Function:            model.Function{Name: "web_search"},
			AnthropicServerSpec: []byte(`{"type":"web_search_20250305","name":"web_search"}`),
		}},
	}
	if !hasGatewayManagedWebSearchTool(anthropicNative) {
		t.Fatal("Anthropic provider-native web_search server tool must be managed")
	}

	ordinaryLookalike := &model.InternalLLMRequest{
		Tools: []model.Tool{{
			Type:     "function",
			Function: model.Function{Name: "x_search"},
		}},
	}
	if hasGatewayManagedWebSearchTool(ordinaryLookalike) {
		t.Fatal("function tools with search-like names must not enable gateway buffering")
	}
}

func TestFindWebSearchCalls(t *testing.T) {
	resp := &model.InternalLLMResponse{
		Choices: []model.Choice{
			{
				Message: &model.Message{
					ToolCalls: []model.ToolCall{
						{ID: "call_1", Function: model.FunctionCall{Name: "web_search", Arguments: `{"query":"golang"}`}},
						{ID: "call_2", Function: model.FunctionCall{Name: "get_weather", Arguments: `{}`}},
					},
				},
			},
			{
				Delta: &model.Message{
					ToolCalls: []model.ToolCall{
						{ID: "call_3", Function: model.FunctionCall{Name: "web_search_preview", Arguments: `{"query":"rust"}`}},
					},
				},
			},
		},
	}
	calls := findWebSearchCalls(resp)
	if len(calls) != 2 {
		t.Fatalf("expected 2 web search calls, got %d", len(calls))
	}
	if calls[0].ID != "call_1" || calls[1].ID != "call_3" {
		t.Fatalf("unexpected calls: %+v", calls)
	}

	if got := findWebSearchCalls(nil); got != nil {
		t.Fatalf("expected nil for nil response, got %v", got)
	}
}

func TestReadWebSearchQuery(t *testing.T) {
	call := model.ToolCall{Function: model.FunctionCall{Arguments: `{"query":"今天天气 北京","extra":1}`}}
	if got := readWebSearchQuery(call); got != "今天天气 北京" {
		t.Fatalf("expected query extraction, got %q", got)
	}

	call2 := model.ToolCall{Function: model.FunctionCall{Arguments: `{"search_query":"golang 教程"}`}}
	if got := readWebSearchQuery(call2); got != "golang 教程" {
		t.Fatalf("expected search_query extraction, got %q", got)
	}

	call3 := model.ToolCall{Function: model.FunctionCall{Arguments: `{"additionalContext":"anthropic docs"}`}}
	if got := readWebSearchQuery(call3); got != "anthropic docs" {
		t.Fatalf("expected additionalContext extraction, got %q", got)
	}

	call4 := model.ToolCall{Function: model.FunctionCall{Arguments: `not json`}}
	if got := readWebSearchQuery(call4); got != "not json" {
		t.Fatalf("expected raw arguments fallback, got %q", got)
	}

	call5 := model.ToolCall{Function: model.FunctionCall{Arguments: `{}`}}
	if got := readWebSearchQuery(call5); got != "" {
		t.Fatalf("expected empty for empty args, got %q", got)
	}
}

func TestParseBingResults(t *testing.T) {
	html := `<html><body>
<ol id="b_results">
<li class="b_algo">
<h2><a href="https://example.com/page1" h="ID=SERP,1">Example Domain Page 1</a></h2>
<div class="b_caption">
<p class="b_lineclamp2">This is the first snippet about example domain.</p>
</div>
</li>
<li class="b_algo">
<h2><a href="https://example.org/page2">Example Org &amp; Page 2</a></h2>
<div class="b_caption">
<p>Second snippet with <strong>bold</strong> text and more content that is long enough to be truncated eventually if it exceeds the maximum snippet length limit of three hundred characters which is quite generous for search results.</p>
</div>
</li>
<li class="b_noresults"></li>
</ol>
</body></html>`

	results := parseBingResults([]byte(html))
	if len(results) != 2 {
		t.Fatalf("expected 2 parsed results, got %d: %+v", len(results), results)
	}
	if results[0].Title != "Example Domain Page 1" {
		t.Fatalf("unexpected title: %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/page1" {
		t.Fatalf("unexpected url: %q", results[0].URL)
	}
	if !strings.Contains(results[0].Snippet, "first snippet") {
		t.Fatalf("unexpected snippet: %q", results[0].Snippet)
	}
	// HTML 实体应被解码
	if !strings.Contains(results[1].Title, "&") {
		t.Fatalf("expected HTML entity decoded title, got %q", results[1].Title)
	}
	// 标签应被剥离
	if strings.Contains(results[1].Snippet, "<strong>") {
		t.Fatalf("expected tags stripped from snippet, got %q", results[1].Snippet)
	}
}

func TestFormatSearchResults(t *testing.T) {
	results := []WebSearchResult{
		{Title: "T1", URL: "https://a.com", Snippet: "S1"},
		{Title: "T2", URL: "https://b.com"},
	}
	text := formatSearchResults("测试", results)
	if !strings.Contains(text, `"测试"`) {
		t.Fatalf("expected query in text: %s", text)
	}
	if !strings.Contains(text, "https://a.com") || !strings.Contains(text, "S1") {
		t.Fatalf("expected result content: %s", text)
	}
	if !strings.Contains(text, "2. T2") {
		t.Fatalf("expected second result: %s", text)
	}

	empty := formatSearchResults("", nil)
	if !strings.Contains(empty, "no results") {
		t.Fatalf("expected no-results marker: %s", empty)
	}
}

func TestBuildWebSearchReplayMessages(t *testing.T) {
	calls := []model.ToolCall{
		{ID: "call_1", Function: model.FunctionCall{Name: "web_search", Arguments: `{"query":"q1"}`}},
		{ID: "call_2", Function: model.FunctionCall{Name: "web_search", Arguments: `{"query":"q2"}`}},
	}
	messages, err := buildWebSearchReplayMessages(calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (assistant + 2 tool), got %d", len(messages))
	}
	if messages[0].Role != "assistant" || len(messages[0].ToolCalls) != 2 {
		t.Fatalf("expected assistant message with 2 tool calls, got %+v", messages[0])
	}
	for i, msg := range messages[1:] {
		if msg.Role != "tool" {
			t.Fatalf("expected tool role, got %q", msg.Role)
		}
		if msg.ToolCallID == nil || *msg.ToolCallID != calls[i].ID {
			t.Fatalf("expected tool_call_id %q, got %v", calls[i].ID, msg.ToolCallID)
		}
		if msg.Content.Content == nil || !strings.Contains(*msg.Content.Content, "no results") {
			t.Fatalf("expected placeholder result text, got %v", msg.Content.Content)
		}
	}

	if _, err := buildWebSearchReplayMessages(nil); err == nil {
		t.Fatal("expected error for empty calls")
	}
}

func TestExecuteWebSearchReplayHandlesEmptyQuery(t *testing.T) {
	calls := []model.ToolCall{
		{ID: "call_1", Function: model.FunctionCall{Name: "web_search", Arguments: `{}`}},
	}
	messages, err := executeWebSearchReplay(context.Background(), calls)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	toolMsg := messages[1]
	if toolMsg.ToolCallIsError == nil || !*toolMsg.ToolCallIsError {
		t.Fatal("expected empty query to mark tool result as error")
	}
	if !strings.Contains(*toolMsg.Content.Content, "empty query") {
		t.Fatalf("expected empty query error text, got %q", *toolMsg.Content.Content)
	}
}

func TestParseBingResultsFromLiveSample(t *testing.T) {
	// 真实抓取的 cn.bing.com 结果页（testdata/bing_sample.html），
	// 验证解析器对线上 HTML 结构（h2 class=""、b_lineclamp 摘要、实体编码）的兼容性。
	sample, err := os.ReadFile(filepath.Join("testdata", "bing_sample.html"))
	if err != nil {
		t.Skipf("no live sample available: %v", err)
	}
	results := parseBingResults(sample)
	if len(results) < 3 {
		t.Fatalf("expected at least 3 parsed results from live sample, got %d: %+v", len(results), results)
	}
	for i, r := range results {
		if r.URL == "" || !strings.HasPrefix(r.URL, "http") {
			t.Fatalf("result %d missing valid URL: %+v", i, r)
		}
		if r.Title == "" {
			t.Fatalf("result %d missing title: %+v", i, r)
		}
		if strings.Contains(r.Title, "<") || strings.Contains(r.URL, " ") {
			t.Fatalf("result %d contains raw HTML or spaces: %+v", i, r)
		}
	}
	t.Logf("parsed %d results from live sample, first: %s - %s", len(results), results[0].Title, results[0].URL)
}

func TestSearchWebLive(t *testing.T) {
	if os.Getenv("OCTOPUS_LIVE_SEARCH") != "1" {
		t.Skip("set OCTOPUS_LIVE_SEARCH=1 to run the live network search test")
	}
	results, err := SearchWeb(context.Background(), "LiveAgent github")
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one live result")
	}
	t.Logf("live search returned %d results, first: %s - %s", len(results), results[0].Title, results[0].URL)
}

func TestStreamEventsAggregationFindsWebSearchCall(t *testing.T) {
	// 模拟 handleStreamResponseV2 中 transform 链路上累积的 StreamEvent，
	// 验证 InternalResponseFromStreamEvents 聚合后能提取 web_search 调用。
	adapter := &openaiOutbound.ChatOutbound{}
	ctx := context.Background()
	var events []model.StreamEvent
	for _, raw := range []string{
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"web_search","arguments":""}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\":\"test\"}"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		evts, err := adapter.TransformStreamEvent(ctx, []byte(raw))
		if err != nil {
			t.Fatalf("TransformStreamEvent(%s): %v", raw, err)
		}
		events = append(events, evts...)
	}
	resp := model.InternalResponseFromStreamEvents(events)
	if resp == nil {
		t.Fatal("expected aggregated response")
	}
	calls := findWebSearchCalls(resp)
	if len(calls) != 1 {
		t.Fatalf("expected 1 web search call, got %d (resp=%+v)", len(calls), resp)
	}
	if calls[0].Function.Name != "web_search" {
		t.Fatalf("unexpected tool name: %q", calls[0].Function.Name)
	}
	if got := readWebSearchQuery(calls[0]); got != "test" {
		t.Fatalf("unexpected query: %q", got)
	}
}

func TestWebSearchBufferWriter(t *testing.T) {
	real := &fakeStreamWriter{}
	w := &webSearchBufferWriter{real: real}

	if _, err := w.Write([]byte("data: {\"a\":1}\n\n")); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if !w.Written() {
		t.Fatal("expected Written() true after write")
	}
	if len(real.writes) != 0 {
		t.Fatal("expected nothing written to real writer before FlushToReal")
	}
	if err := w.FlushToReal(); err != nil {
		t.Fatalf("flush error: %v", err)
	}
	if len(real.writes) != 1 || string(real.writes[0]) != "data: {\"a\":1}\n\n" {
		t.Fatalf("expected buffered payload flushed to real writer, got %v", real.writes)
	}
	// 二次 flush 无内容
	if err := w.FlushToReal(); err != nil {
		t.Fatalf("second flush error: %v", err)
	}
	if len(real.writes) != 1 {
		t.Fatalf("expected no extra write, got %d", len(real.writes))
	}
}

type fakeStreamWriter struct {
	writes [][]byte
}

func (f *fakeStreamWriter) Write(data []byte) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), data...))
	return len(data), nil
}

func (f *fakeStreamWriter) Flush() {}

func (f *fakeStreamWriter) Written() bool { return len(f.writes) > 0 }

func (f *fakeStreamWriter) Header() http.Header { return nil }

func (f *fakeStreamWriter) WriteHeader(code int) {}
