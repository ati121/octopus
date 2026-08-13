package op

import (
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupUpstreamRequestTestDB(t *testing.T) {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "upstream-request.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	settingCache.Clear()
	if err := settingRefreshCache(t.Context()); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	// 记忆化缓存是包级变量，跨用例会串味，每个用例重置。
	upstreamGlobalHeadersCache.state.Store(nil)
	upstreamModelHeaderRuleCache.state.Store(nil)
	upstreamModelParamRuleCache.state.Store(nil)
}

func headerValue(headers []model.CustomHeader, key string) (string, bool) {
	for _, header := range headers {
		if header.HeaderKey == key {
			return header.HeaderValue, true
		}
	}
	return "", false
}

// 默认值下（"[]" / "{}"）三个入口都必须返回空，保证老部署行为不变。
func TestUpstreamRequestDefaultsAreEmpty(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	if headers := UpstreamGlobalHeaders(); len(headers) != 0 {
		t.Fatalf("expected no global headers by default, got %+v", headers)
	}
	if headers := UpstreamModelHeadersFor("gpt-4o"); len(headers) != 0 {
		t.Fatalf("expected no model headers by default, got %+v", headers)
	}
	if chain := UpstreamParamOverrideChain("gpt-4o"); len(chain) != 0 {
		t.Fatalf("expected empty override chain by default, got %v", chain)
	}
}

func TestUpstreamGlobalHeaders(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	value := `[{"header_key":"User-Agent","header_value":"Codex Desktop/26.7"},{"header_key":"X-Trace","header_value":"on"}]`
	if err := SettingSetString(model.SettingKeyUpstreamGlobalHeaders, value); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}

	headers := UpstreamGlobalHeaders()
	if len(headers) != 2 {
		t.Fatalf("expected 2 global headers, got %d", len(headers))
	}
	if got, ok := headerValue(headers, "User-Agent"); !ok || got != "Codex Desktop/26.7" {
		t.Fatalf("User-Agent = %q (found=%v)", got, ok)
	}
}

// 解析失败时不能让中继挂掉，返回空并继续。
func TestUpstreamGlobalHeadersIgnoresBrokenValue(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	if err := SettingSetString(model.SettingKeyUpstreamGlobalHeaders, `[{"header_key":`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	if headers := UpstreamGlobalHeaders(); len(headers) != 0 {
		t.Fatalf("broken value should yield no headers, got %+v", headers)
	}
}

func TestUpstreamModelHeadersFor(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	value := `[
		{"models":"gpt-4*,o3-*","headers":[{"header_key":"X-Family","header_value":"openai"}]},
		{"models":"claude-*","headers":[{"header_key":"X-Family","header_value":"anthropic"}]},
		{"models":"openai/*","headers":[{"header_key":"X-Prefixed","header_value":"yes"}]}
	]`
	if err := SettingSetString(model.SettingKeyUpstreamModelHeaderRules, value); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}

	cases := []struct {
		model     string
		wantKey   string
		wantValue string
		wantCount int
	}{
		{"gpt-4o-mini", "X-Family", "openai", 1},
		{"o3-mini", "X-Family", "openai", 1},
		{"claude-sonnet-4", "X-Family", "anthropic", 1},
		{"openai/gpt-4o", "X-Prefixed", "yes", 1},
		{"gemini-2.5-pro", "", "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			headers := UpstreamModelHeadersFor(tc.model)
			if len(headers) != tc.wantCount {
				t.Fatalf("headers for %q = %+v, want %d entries", tc.model, headers, tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}
			got, ok := headerValue(headers, tc.wantKey)
			if !ok || got != tc.wantValue {
				t.Fatalf("%s = %q (found=%v), want %q", tc.wantKey, got, ok, tc.wantValue)
			}
		})
	}
}

// 多条规则命中同一模型：按声明顺序拼接，调用方顺序 Set 后后者生效。
func TestUpstreamModelHeadersForMultipleMatchesKeepOrder(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	value := `[
		{"models":"gpt-*","headers":[{"header_key":"X-Tier","header_value":"first"}]},
		{"models":"gpt-4*","headers":[{"header_key":"X-Tier","header_value":"second"}]}
	]`
	if err := SettingSetString(model.SettingKeyUpstreamModelHeaderRules, value); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}

	headers := UpstreamModelHeadersFor("gpt-4o")
	if len(headers) != 2 {
		t.Fatalf("expected both matching rules to contribute, got %+v", headers)
	}
	if headers[0].HeaderValue != "first" || headers[1].HeaderValue != "second" {
		t.Fatalf("rules must keep declaration order, got %+v", headers)
	}
}

func TestUpstreamParamOverrideChain(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	if err := SettingSetString(model.SettingKeyUpstreamGlobalParamOverride, `{"temperature":0.2}`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	rules := `[
		{"models":"claude-*","param_override":{"temperature":0.5}},
		{"models":"gpt-4*","param_override":{"top_p":0.9}}
	]`
	if err := SettingSetString(model.SettingKeyUpstreamModelParamRules, rules); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}

	chain := UpstreamParamOverrideChain("claude-sonnet-4")
	if len(chain) != 2 {
		t.Fatalf("expected global + 1 matching rule, got %d layers: %v", len(chain), chain)
	}
	if string(chain[0]) != `{"temperature":0.2}` {
		t.Fatalf("first layer must be global, got %s", chain[0])
	}
	if string(chain[1]) != `{"temperature":0.5}` {
		t.Fatalf("second layer must be the matching rule, got %s", chain[1])
	}

	// 不命中任何规则时只剩全局层。
	chain = UpstreamParamOverrideChain("gemini-2.5-pro")
	if len(chain) != 1 || string(chain[0]) != `{"temperature":0.2}` {
		t.Fatalf("expected only the global layer, got %v", chain)
	}
}

// 等价于「无覆盖」的写法都不进链，保证未配置时请求体字节不被改写。
func TestUpstreamParamOverrideChainSkipsBlankGlobal(t *testing.T) {
	for _, value := range []string{"", "  ", "{}", "null"} {
		t.Run(value, func(t *testing.T) {
			setupUpstreamRequestTestDB(t)
			if err := SettingSetString(model.SettingKeyUpstreamGlobalParamOverride, value); err != nil {
				t.Fatalf("SettingSetString failed: %v", err)
			}
			if chain := UpstreamParamOverrideChain("gpt-4o"); len(chain) != 0 {
				t.Fatalf("blank global override %q should not enter the chain, got %v", value, chain)
			}
		})
	}
}

// 规则里 param_override 缺失、为 null 或为空对象的条目不进链。
func TestUpstreamParamOverrideChainSkipsRuleWithoutOverride(t *testing.T) {
	rules := []string{
		`[{"models":"gpt-4*"}]`,
		`[{"models":"gpt-4*","param_override":null}]`,
		`[{"models":"gpt-4*","param_override":{}}]`,
	}
	for _, rule := range rules {
		t.Run(rule, func(t *testing.T) {
			setupUpstreamRequestTestDB(t)
			if err := SettingSetString(model.SettingKeyUpstreamModelParamRules, rule); err != nil {
				t.Fatalf("SettingSetString failed: %v", err)
			}
			if chain := UpstreamParamOverrideChain("gpt-4o"); len(chain) != 0 {
				t.Fatalf("rule without a real override should not enter the chain, got %v", chain)
			}
		})
	}
}

// 记忆化：原文不变时复用同一份解析结果；原文变化后立刻生效。
func TestUpstreamSettingsMemoization(t *testing.T) {
	setupUpstreamRequestTestDB(t)

	value := `[{"header_key":"X-A","header_value":"1"}]`
	if err := SettingSetString(model.SettingKeyUpstreamGlobalHeaders, value); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}

	first := UpstreamGlobalHeaders()
	second := UpstreamGlobalHeaders()
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected header counts: %d / %d", len(first), len(second))
	}
	// 同一份底层数组说明没有重复解析。
	if &first[0] != &second[0] {
		t.Fatal("expected the parsed value to be reused while the raw string is unchanged")
	}

	if err := SettingSetString(model.SettingKeyUpstreamGlobalHeaders, `[{"header_key":"X-B","header_value":"2"}]`); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	updated := UpstreamGlobalHeaders()
	if len(updated) != 1 || updated[0].HeaderKey != "X-B" {
		t.Fatalf("setting update should take effect immediately, got %+v", updated)
	}
}
