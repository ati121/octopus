package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	openaiInbound "github.com/bestruirui/octopus/internal/transformer/inbound/openai"
)

// 复现 bug 报告的复现步骤：OpenAI 格式 chat/completions 带内置 web_search 工具，
// 经 octopus 转发到 Anthropic 端点（MiniMax）。
func TestEndToEndOpenAIWebSearchToAnthropic(t *testing.T) {
	body := []byte(`{
		"model": "MiniMax-M2.7",
		"messages": [{"role":"user","content":"搜索一下魔兽世界时光服的最新消息，给出来源"}],
		"max_tokens": 500,
		"tools": [{"type": "web_search", "web_search": {"max_results": 3}}]
	}`)

	in := &openaiInbound.ChatInbound{}
	internal, err := in.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	if len(internal.Tools) != 1 || internal.Tools[0].Type != "web_search" {
		t.Fatalf("inbound tools: %#v", internal.Tools)
	}

	out := &MessageOutbound{}
	req, err := out.TransformRequest(context.Background(), internal, "https://api.minimaxi.com/anthropic/v1", "k")
	if err != nil {
		t.Fatalf("outbound: %v", err)
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var wire struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal outbound body: %v", err)
	}
	if len(wire.Tools) != 1 {
		t.Fatalf("expected 1 tool on the wire, got %d — body=%s", len(wire.Tools), raw)
	}
	if wire.Tools[0]["type"] != "web_search_20250305" {
		t.Fatalf("wrong tool type: %v", wire.Tools[0]["type"])
	}
	if wire.Tools[0]["name"] != "web_search" {
		t.Fatalf("wrong tool name: %v", wire.Tools[0]["name"])
	}
	t.Logf("outbound tools = %v", wire.Tools)
	t.Logf("anthropic-beta = %q", req.Header.Get("anthropic-beta"))
}
