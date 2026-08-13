package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingValidateUpstreamGlobalHeaders(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"空值放行", "", false},
		{"空白放行", "   ", false},
		{"空数组", "[]", false},
		{"单个请求头", `[{"header_key":"User-Agent","header_value":"Codex/1.0"}]`, false},
		{"允许空的头值", `[{"header_key":"X-Trace","header_value":""}]`, false},
		{"空的头名被拒绝", `[{"header_key":"","header_value":"v"}]`, true},
		{"空白头名被拒绝", `[{"header_key":"  ","header_value":"v"}]`, true},
		{"对象而非数组", `{"header_key":"a","header_value":"b"}`, true},
		{"标量", `"nope"`, true},
		{"畸形 JSON", `[{`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := &Setting{Key: SettingKeyUpstreamGlobalHeaders, Value: tc.value}
			err := setting.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestSettingValidateUpstreamModelHeaderRules(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"空值放行", "", false},
		{"空数组", "[]", false},
		{"合法规则", `[{"models":"gpt-4*,o3-*","headers":[{"header_key":"X-A","header_value":"1"}]}]`, false},
		{"允许规则头为空", `[{"models":"gpt-4*","headers":[]}]`, false},
		{"models 为空被拒绝", `[{"models":"","headers":[{"header_key":"X-A","header_value":"1"}]}]`, true},
		{"models 全空白被拒绝", `[{"models":"  ","headers":[]}]`, true},
		{"规则内空头名被拒绝", `[{"models":"gpt-4*","headers":[{"header_key":"","header_value":"1"}]}]`, true},
		{"对象而非数组", `{"models":"gpt-4*"}`, true},
		{"畸形 JSON", `[{"models":]`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := &Setting{Key: SettingKeyUpstreamModelHeaderRules, Value: tc.value}
			err := setting.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestSettingValidateUpstreamGlobalParamOverride(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"空值放行", "", false},
		{"空白放行", "  ", false},
		{"空对象", "{}", false},
		{"普通对象", `{"temperature":0.7,"max_tokens":4096}`, false},
		{"嵌套对象", `{"thinking":{"type":"enabled","budget_tokens":1024}}`, false},
		{"数组被拒绝", `[{"temperature":0.7}]`, true},
		{"标量被拒绝", `123`, true},
		{"字符串被拒绝", `"temperature"`, true},
		{"畸形 JSON", `{"temperature":}`, true},
		{"含 model 被拒绝", `{"model":"gpt-4o"}`, true},
		{"含 MODEL 大写也被拒绝", `{"MODEL":"gpt-4o"}`, true},
		{"嵌套里的 model 允许", `{"metadata":{"model":"gpt-4o"}}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := &Setting{Key: SettingKeyUpstreamGlobalParamOverride, Value: tc.value}
			err := setting.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestSettingValidateUpstreamModelParamRules(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"空值放行", "", false},
		{"空数组", "[]", false},
		{"合法规则", `[{"models":"claude-*","param_override":{"temperature":0.5}}]`, false},
		{"允许覆盖为空", `[{"models":"claude-*"}]`, false},
		{"允许覆盖为空对象", `[{"models":"claude-*","param_override":{}}]`, false},
		{"models 为空被拒绝", `[{"models":"","param_override":{"temperature":0.5}}]`, true},
		{"覆盖为数组被拒绝", `[{"models":"claude-*","param_override":[1,2]}]`, true},
		{"覆盖含 model 被拒绝", `[{"models":"claude-*","param_override":{"model":"x"}}]`, true},
		{"第二条规则出错也能拦住", `[{"models":"a","param_override":{}},{"models":"b","param_override":{"model":"x"}}]`, true},
		{"畸形 JSON", `[{"models":`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := &Setting{Key: SettingKeyUpstreamModelParamRules, Value: tc.value}
			err := setting.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate(%q) error = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

// 规则报错需要带上是第几条，方便前端把服务端消息直接展示给用户。
func TestUpstreamRuleErrorMentionsRuleIndex(t *testing.T) {
	setting := &Setting{
		Key:   SettingKeyUpstreamModelParamRules,
		Value: `[{"models":"a","param_override":{}},{"models":"","param_override":{}}]`,
	}
	err := setting.Validate()
	if err == nil {
		t.Fatal("expected error for empty models in second rule")
	}
	if !strings.Contains(err.Error(), "rule 2") {
		t.Fatalf("error should mention rule 2, got: %v", err)
	}
}

func TestParseUpstreamHeaderRules(t *testing.T) {
	rules, err := ParseUpstreamHeaderRules(`[{"models":"gpt-4*","headers":[{"header_key":"X-A","header_value":"1"}]}]`)
	if err != nil {
		t.Fatalf("ParseUpstreamHeaderRules failed: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Models != "gpt-4*" {
		t.Fatalf("unexpected models: %q", rules[0].Models)
	}
	if len(rules[0].Headers) != 1 || rules[0].Headers[0].HeaderKey != "X-A" || rules[0].Headers[0].HeaderValue != "1" {
		t.Fatalf("unexpected headers: %+v", rules[0].Headers)
	}
}

// param_override 用 json.RawMessage 承载，回读应拿到原始对象而非被转义的字符串。
func TestParseUpstreamParamRulesKeepsRawJSON(t *testing.T) {
	rules, err := ParseUpstreamParamRules(`[{"models":"claude-*","param_override":{"thinking":{"budget_tokens":1024}}}]`)
	if err != nil {
		t.Fatalf("ParseUpstreamParamRules failed: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	var decoded map[string]any
	if err := json.Unmarshal(rules[0].ParamOverride, &decoded); err != nil {
		t.Fatalf("param override should be raw JSON object: %v", err)
	}
	thinking, ok := decoded["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested thinking object, got %#v", decoded["thinking"])
	}
	if thinking["budget_tokens"] != float64(1024) {
		t.Fatalf("unexpected budget_tokens: %#v", thinking["budget_tokens"])
	}
}

func TestParseUpstreamHeadersEmpty(t *testing.T) {
	headers, err := ParseUpstreamHeaders("  ")
	if err != nil {
		t.Fatalf("ParseUpstreamHeaders failed: %v", err)
	}
	if headers != nil {
		t.Fatalf("expected nil headers for blank value, got %+v", headers)
	}
}

// 新增的 4 个 key 必须在 DefaultSettings 里，否则启动不会自动补种，
// 而 SettingSetString 无法 INSERT 只能 UPDATE，会导致这些 key 永远写不进去。
func TestDefaultSettingsIncludeUpstreamKeys(t *testing.T) {
	want := map[SettingKey]string{
		SettingKeyUpstreamGlobalHeaders:       "[]",
		SettingKeyUpstreamModelHeaderRules:    "[]",
		SettingKeyUpstreamGlobalParamOverride: "{}",
		SettingKeyUpstreamModelParamRules:     "[]",
	}
	defaults := make(map[SettingKey]string)
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}
	for key, value := range want {
		got, ok := defaults[key]
		if !ok {
			t.Fatalf("DefaultSettings missing key %q", key)
		}
		if got != value {
			t.Fatalf("default for %q = %q, want %q", key, got, value)
		}
		setting := &Setting{Key: key, Value: got}
		if err := setting.Validate(); err != nil {
			t.Fatalf("default value for %q must pass validation: %v", key, err)
		}
	}
}
