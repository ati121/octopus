package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// OpenAI Chat 的内置 `{"type":"web_search"}` 工具没有 AnthropicServerSpec，
// 以前会被 convertTools 整条丢弃（missing_spec 告警），上游因此不执行联网搜索，
// 响应里也就没有引用来源。补齐后必须产出 Anthropic 要求的版本化 type + name。
func TestConvertToolsSynthesizesWebSearchSpec(t *testing.T) {
	cases := []struct {
		name     string
		toolType string
		wantType string
		wantName string
	}{
		{name: "openai chat web_search", toolType: "web_search", wantType: "web_search_20250305", wantName: "web_search"},
		{name: "responses preview", toolType: "web_search_preview", wantType: "web_search_20250305", wantName: "web_search"},
		{name: "gemini server_search", toolType: "server_search", wantType: "web_search_20250305", wantName: "web_search"},
		{name: "code execution", toolType: "code_execution", wantType: "code_execution_20250522", wantName: "code_execution"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := convertTools([]model.Tool{{Type: tc.toolType}})
			if len(tools) != 1 {
				t.Fatalf("expected the server tool to survive conversion, got %d tools", len(tools))
			}
			got := tools[0]
			if got.Type != tc.wantType {
				t.Fatalf("wire type = %q, want %q", got.Type, tc.wantType)
			}
			if got.Name != tc.wantName {
				t.Fatalf("wire name = %q, want %q", got.Name, tc.wantName)
			}

			// spec 必须能序列化出上游认得的 type+name，否则 MiniMax 一类的
			// Anthropic 兼容端点会拒绝或静默不搜索。
			body, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal tool: %v", err)
			}
			var wire map[string]any
			if err := json.Unmarshal(body, &wire); err != nil {
				t.Fatalf("unmarshal tool wire body: %v", err)
			}
			if wire["type"] != tc.wantType {
				t.Fatalf("serialized type = %v, want %q", wire["type"], tc.wantType)
			}
			if wire["name"] != tc.wantName {
				t.Fatalf("serialized name = %v, want %q", wire["name"], tc.wantName)
			}
		})
	}
}

// 已经带 AnthropicServerSpec 的工具走原有原文回放路径，补齐逻辑不得介入，
// 否则 max_uses / allowed_domains 这类 spec 专有字段会被抹掉。
func TestConvertToolsPrefersInboundSpec(t *testing.T) {
	raw := `{"type":"web_search_20250305","name":"web_search","max_uses":5,"allowed_domains":["example.com"]}`
	tools := convertTools([]model.Tool{{
		Type:                "web_search_20250305",
		Function:            model.Function{Name: "web_search"},
		AnthropicServerSpec: json.RawMessage(raw),
	}})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	body, err := json.Marshal(tools[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{"max_uses", "allowed_domains", "example.com"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("inbound spec field %q was lost: %s", want, body)
		}
	}
}

// 无法映射到 Anthropic 服务端工具族的类型仍然按原逻辑丢弃，
// 不能把上游不认识的 type 发出去换成 400。
func TestConvertToolsDropsUnmappableServerTool(t *testing.T) {
	tools := convertTools([]model.Tool{{Type: "totally_unknown_tool"}})
	if len(tools) != 0 {
		t.Fatalf("expected unmappable server tool to be dropped, got %#v", tools)
	}
}

// 普通 function 工具不受影响。
func TestConvertToolsKeepsFunctionTools(t *testing.T) {
	tools := convertTools([]model.Tool{{
		Type: "function",
		Function: model.Function{
			Name:        "get_weather",
			Description: "look up weather",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "get_weather" || tools[0].Type != "" {
		t.Fatalf("function tool mangled: %#v", tools[0])
	}
}
