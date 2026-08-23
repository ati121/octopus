package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/iolimit"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// errWebSearchReplayNeeded 是内部哨兵错误：上游响应中包含未被执行的
// provider-native web search 调用（web_search / web_search_preview /
// Anthropic web_search_* server tool），网关需要自己执行搜索后重放请求。
var errWebSearchReplayNeeded = fmt.Errorf("web search replay needed")

// webSearchToolNames 覆盖 LiveAgent / Claude Code / Codex / Gemini 客户端
// 注入的 provider-native 搜索工具名（function tool 与 server tool 两种形态）。
var webSearchToolNames = []string{
	"web_search",
	"websearch",
	"web_search_preview",
	"builtin_web_search",
	"x_search",
	"x_keyword_search",
	"x_semantic_search",
}

// isWebSearchToolName 判断工具名是否为 provider-native 搜索工具。
func isWebSearchToolName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}
	for _, candidate := range webSearchToolNames {
		if name == candidate {
			return true
		}
	}
	// Anthropic server tool 版本化命名：web_search_20250305 / web_search_20260318 ...
	if strings.HasPrefix(name, "web_search_") {
		return true
	}
	if strings.HasPrefix(name, "x_search_") || strings.HasPrefix(name, "x_keyword_") {
		return true
	}
	return false
}

// isWebSearchToolType 判断工具 type（OpenAI Responses 原生 web_search 等）。
func isWebSearchToolType(toolType string) bool {
	toolType = strings.TrimSpace(strings.ToLower(toolType))
	if toolType == "" {
		return false
	}
	if toolType == "web_search" {
		return true
	}
	if strings.HasPrefix(toolType, "web_search_") {
		return true
	}
	return false
}

// hasWebSearchTool 宽泛判断请求是否声明了搜索工具，仅用于诊断与兼容检查。
// 是否允许网关拦截、缓冲和重放必须使用更严格的
// hasGatewayManagedWebSearchTool，不能仅凭普通 function 名称决定。
func hasWebSearchTool(req *model.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	for _, tool := range req.Tools {
		if isWebSearchToolName(tool.Function.Name) || isWebSearchToolType(tool.Type) {
			return true
		}
	}
	return false
}

// isGatewayManagedWebSearchTool reports whether a request tool is a
// provider-native search tool that Octopus is allowed to intercept and replay.
//
// A regular OpenAI function named "web_search" is deliberately excluded. Many
// clients (Hermes included) register their own function with that name and
// expect to execute it themselves. Looking at the function name alone would
// make the relay buffer the entire stream before it could decide whether a
// search call occurred.
func isGatewayManagedWebSearchTool(tool model.Tool) bool {
	if isWebSearchToolType(tool.Type) {
		return true
	}

	// Anthropic server tools retain their raw provider spec across the internal
	// request conversion. Accept the spec as a second, explicit native-tool
	// signal, while keeping ordinary `type:function` tools out of this path.
	if len(tool.AnthropicServerSpec) > 0 {
		var spec struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(tool.AnthropicServerSpec, &spec); err == nil {
			return isWebSearchToolType(spec.Type) || isWebSearchToolName(spec.Name)
		}
	}
	return false
}

// hasGatewayManagedWebSearchTool is the buffering gate used by the relay.
// Keep it narrower than hasWebSearchTool: the latter is a broad declaration
// detector, whereas this function controls whether the downstream stream may
// be withheld for a possible gateway-side replay.
func hasGatewayManagedWebSearchTool(req *model.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	for _, tool := range req.Tools {
		if isGatewayManagedWebSearchTool(tool) {
			return true
		}
	}
	return false
}

// findWebSearchCalls 从响应中提取所有 web_search 工具调用。
// 覆盖非流式（Choices[].Message.ToolCalls）与流式累积（Choices[].Delta.ToolCalls）。
func findWebSearchCalls(resp *model.InternalLLMResponse) []model.ToolCall {
	if resp == nil {
		return nil
	}
	var calls []model.ToolCall
	for _, choice := range resp.Choices {
		var toolCalls []model.ToolCall
		if choice.Message != nil {
			toolCalls = choice.Message.ToolCalls
		} else if choice.Delta != nil {
			toolCalls = choice.Delta.ToolCalls
		}
		for _, call := range toolCalls {
			if isWebSearchToolName(call.Function.Name) || isWebSearchToolType(call.Type) {
				calls = append(calls, call)
			}
		}
	}
	return calls
}

// readWebSearchQuery 提取搜索关键词：优先 query/search_query，
// 兼容 Anthropic 风格的 additionalContext。
func readWebSearchQuery(call model.ToolCall) string {
	args := strings.TrimSpace(call.Function.Arguments)
	if args == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return args // 非 JSON 参数，整段作为查询词
	}
	for _, key := range []string{"query", "search_query", "additionalContext", "q"} {
		if value, ok := parsed[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if compact, err := json.Marshal(parsed); err == nil {
		if text := strings.TrimSpace(string(compact)); text != "" && text != "{}" {
			return text
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 搜索结果
// ---------------------------------------------------------------------------

// WebSearchResult 表示一条搜索结果。
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

const (
	webSearchMaxResults      = 6
	webSearchMaxSnippetLen   = 300
	webSearchMaxResultText   = 6000
	webSearchHTTPTimeout     = 12 * time.Second
	webSearchMaxResponseSize = 2 * 1024 * 1024
)

// SearchWeb 执行一次网络搜索（默认 Bing，无需 API key）。
func SearchWeb(ctx context.Context, query string) ([]WebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	client, err := newWebSearchHTTPClient()
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("count", fmt.Sprint(webSearchMaxResults))
	params.Set("setlang", "zh-hans")
	params.Set("mkt", "zh-CN")
	searchURL := "https://www.bing.com/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := iolimit.ReadAll(resp.Body, webSearchMaxResponseSize)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	return parseBingResults(body), nil
}

// newWebSearchHTTPClient 返回用于搜索的 HTTP client（默认直连，不跟随系统代理）。
func newWebSearchHTTPClient() (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: webSearchHTTPTimeout}, nil
	}
	cloned := transport.Clone()
	cloned.Proxy = nil
	return &http.Client{Transport: cloned, Timeout: webSearchHTTPTimeout}, nil
}

// bingAlgoBlockRE 匹配 Bing 的 li.b_algo 结果块。
var bingAlgoBlockRE = regexp.MustCompile(`(?is)<li[^>]*class="[^"]*b_algo[^"]*"[^>]*>(.*?)</li>`)

// bingAnchorRE 提取结果块内的 h2 > a href title。
var bingAnchorRE = regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>\s*</h2>`)

// bingSnippetRE 提取结果块内的摘要 p（b_lineclamp 是 Bing 摘要段落的标准 class，
// 限定 class 避免误匹配 SVG path 等其它 <p 前缀标签）。
var bingSnippetRE = regexp.MustCompile(`(?is)<p class="b_lineclamp[^"]*"[^>]*>(.*?)</p>`)

// stripTagsRE 剥离 HTML 标签。
var stripTagsRE = regexp.MustCompile(`(?s)<[^>]+>`)

// parseBingResults 解析 Bing 搜索结果 HTML。
func parseBingResults(body []byte) []WebSearchResult {
	results := make([]WebSearchResult, 0, webSearchMaxResults)
	for _, block := range bingAlgoBlockRE.FindAllSubmatch(body, webSearchMaxResults) {
		if len(block) < 2 {
			continue
		}
		blockHTML := string(block[1])

		anchor := bingAnchorRE.FindStringSubmatch(blockHTML)
		if len(anchor) < 3 {
			continue
		}
		rawURL := strings.TrimSpace(anchor[1])
		lower := strings.ToLower(rawURL)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			continue
		}
		title := cleanSearchText(anchor[2])

		snippet := ""
		if sm := bingSnippetRE.FindStringSubmatch(blockHTML); len(sm) > 1 {
			snippet = cleanSearchText(sm[1])
			if len(snippet) > webSearchMaxSnippetLen {
				snippet = snippet[:webSearchMaxSnippetLen] + "..."
			}
		}

		results = append(results, WebSearchResult{
			Title:   title,
			URL:     rawURL,
			Snippet: snippet,
		})
		if len(results) >= webSearchMaxResults {
			break
		}
	}
	return results
}

// cleanSearchText 清理提取出的文本：去标签、HTML 实体解码、压缩空白。
func cleanSearchText(raw string) string {
	text := stripTagsRE.ReplaceAllString(raw, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

// ---------------------------------------------------------------------------
// tool result 构造
// ---------------------------------------------------------------------------

// buildWebSearchReplayMessages 为一次重放构造消息：
//   - assistant（回放原始 tool_calls，保证上游上下文一致）
//   - tool（每个搜索调用一条，内容为搜索结果文本）
func buildWebSearchReplayMessages(calls []model.ToolCall) ([]model.Message, error) {
	if len(calls) == 0 {
		return nil, fmt.Errorf("no web search calls to replay")
	}

	assistantMsg := model.Message{
		Role:      "assistant",
		ToolCalls: calls,
	}

	messages := make([]model.Message, 0, len(calls)+1)
	messages = append(messages, assistantMsg)

	for _, call := range calls {
		if call.ID == "" {
			continue
		}
		query := readWebSearchQuery(call)
		text := formatSearchResults(query, nil)
		toolMsg := model.Message{
			Role:            "tool",
			ToolCallID:      &call.ID,
			Content:         model.MessageContent{Content: &text},
			ToolCallIsError: boolPtr(false),
		}
		messages = append(messages, toolMsg)
	}
	if len(messages) == 1 {
		return nil, fmt.Errorf("web search calls missing tool_call_id")
	}
	return messages, nil
}

// filterStreamEventsForAggregation 去掉 Done 事件后返回新切片；
// InternalResponseFromStreamEvents 遇到 Done 会立即返回空响应，必须前置过滤。
func filterStreamEventsForAggregation(events []model.StreamEvent) []model.StreamEvent {
	out := make([]model.StreamEvent, 0, len(events))
	for _, ev := range events {
		if ev.Kind == model.StreamEventKindDone {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// firstStreamEventSummary 返回前 N 个 StreamEvent 的摘要（kind@index:role），用于诊断。
func firstStreamEventSummary(events []model.StreamEvent, limit int) string {
	if len(events) == 0 {
		return "(none)"
	}
	if limit <= 0 {
		limit = 3
	}
	kinds := make([]string, 0, limit)
	for i, ev := range events {
		if i >= limit {
			break
		}
		kind := string(ev.Kind)
		if kind == "" {
			kind = "?"
		}
		kinds = append(kinds, fmt.Sprintf("%s@%d:%s", kind, ev.Index, ev.Role))
	}
	return strings.Join(kinds, " ")
}

// formatSearchResults 把搜索结果格式化为给模型看的文本。
func formatSearchResults(query string, results []WebSearchResult) string {
	var b strings.Builder
	if query != "" {
		fmt.Fprintf(&b, "Web search results for query %q:\n\n", query)
	} else {
		b.WriteString("Web search results:\n\n")
	}
	if len(results) == 0 {
		b.WriteString("(no results found)")
		return b.String()
	}
	for i, r := range results {
		if b.Len() >= webSearchMaxResultText {
			break
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.Title)
		fmt.Fprintf(&b, "   %s\n", r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// 设置
// ---------------------------------------------------------------------------

// webSearchEnabled 读取网关级开关，默认开启。
func webSearchEnabled() bool {
	value, err := op.SettingGetString(dbmodel.SettingKeyWebSearchEnabled)
	if err != nil {
		return true
	}
	value = strings.TrimSpace(strings.ToLower(value))
	return value != "" && value != "0" && value != "false" && value != "off"
}

// webSearchMaxRounds 读取最大重放轮数（默认 10，上限 100）。
func webSearchMaxRounds() int {
	value, err := op.SettingGetInt(dbmodel.SettingKeyWebSearchMaxRounds)
	if err != nil || value <= 0 {
		return dbmodel.DefaultWebSearchMaxRounds
	}
	if value > dbmodel.MaxWebSearchMaxRounds {
		return dbmodel.MaxWebSearchMaxRounds
	}
	return int(value)
}

// ---------------------------------------------------------------------------
// 缓冲 writer（流式拦截用）
// ---------------------------------------------------------------------------

// webSearchBufferWriter 缓冲所有写入，不落真实 writer。
// StreamProcessor 认为已写入（Written=true 且有数据），
// 最终由调用方决定是否把缓冲内容整体写出（无搜索调用时）或丢弃（重放时）。
type webSearchBufferWriter struct {
	real StreamWriter
	buf  bytes.Buffer
}

func (w *webSearchBufferWriter) Write(data []byte) (int, error) {
	return w.buf.Write(data)
}

func (w *webSearchBufferWriter) Flush() {}

func (w *webSearchBufferWriter) Written() bool {
	return w.buf.Len() > 0
}

func (w *webSearchBufferWriter) Header() http.Header {
	return w.real.Header()
}

func (w *webSearchBufferWriter) WriteHeader(code int) {
	w.real.WriteHeader(code)
}

func (w *webSearchBufferWriter) FlushToReal() error {
	if w.buf.Len() == 0 {
		return nil
	}
	if _, err := w.real.Write(w.buf.Bytes()); err != nil {
		return err
	}
	w.buf.Reset()
	w.real.Flush()
	return nil
}
