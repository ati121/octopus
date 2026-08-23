package model

import "testing"

func TestValidateSiteModelFilterRegex(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{name: "empty", pattern: "", wantErr: false},
		{name: "blank", pattern: "   ", wantErr: false},
		{name: "plain", pattern: "^claude-", wantErr: false},
		{name: "inline flags", pattern: "(?i)^Claude-", wantErr: false},
		{name: "unclosed group", pattern: "^(claude", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSiteModelFilterRegex(tc.pattern)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for pattern %q", tc.pattern)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for pattern %q: %v", tc.pattern, err)
			}
		})
	}
}

func TestCompileSiteModelFilterRegexBlankReturnsNil(t *testing.T) {
	re, err := CompileSiteModelFilterRegex("  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if re != nil {
		t.Fatalf("expected nil regexp for blank pattern")
	}
	if !SiteModelFilterAllows(re, "anything") {
		t.Fatalf("nil regexp must allow every model")
	}
}

func TestSiteModelFilterAllowsHonorsInlineFlags(t *testing.T) {
	re, err := CompileSiteModelFilterRegex("(?i)^claude-")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if !SiteModelFilterAllows(re, "Claude-Opus-5") {
		t.Fatalf("expected case-insensitive match")
	}
	if SiteModelFilterAllows(re, "deepseek-v4") {
		t.Fatalf("expected non-match")
	}
}

func TestApplySiteModelFiltersRecomputesDisabled(t *testing.T) {
	models := []SiteModel{
		{GroupKey: "default", ModelName: "claude-opus-5", Disabled: true},
		{GroupKey: "default", ModelName: "deepseek-v4", Disabled: false},
		{GroupKey: "vip", ModelName: "gpt-5", Disabled: true},
	}
	changed := ApplySiteModelFilters(models, map[string]string{"default": "^claude-"})
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if models[0].Disabled {
		t.Fatalf("matched model must be enabled")
	}
	if !models[1].Disabled {
		t.Fatalf("unmatched model must be disabled")
	}
	// vip 没有配置正则，其手动停用状态必须原样保留。
	if !models[2].Disabled {
		t.Fatalf("group without a filter must keep its existing state")
	}
}

// 空正则表示「不筛选」。它只在保存那一刻由 op.UpdateSiteGroupModelFilter 触发全部启用；
// 同步链路必须原样跳过，否则每轮同步都会把用户手动停用的模型重新翻上来。
func TestApplySiteModelFiltersSkipsBlankAndInvalidPatterns(t *testing.T) {
	models := []SiteModel{
		{GroupKey: "default", ModelName: "claude-opus-5", Disabled: true},
		{GroupKey: "vip", ModelName: "gpt-5", Disabled: true},
	}
	changed := ApplySiteModelFilters(models, map[string]string{
		"default": "",
		"vip":     "^(gpt",
	})
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	for i := range models {
		if !models[i].Disabled {
			t.Fatalf("model %q must keep its existing disabled state", models[i].ModelName)
		}
	}
}

func TestApplySiteModelFiltersNormalizesGroupKey(t *testing.T) {
	models := []SiteModel{{GroupKey: "", ModelName: "claude-opus-5", Disabled: true}}
	if changed := ApplySiteModelFilters(models, map[string]string{"": "^claude-"}); changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if models[0].Disabled {
		t.Fatalf("expected model to be enabled after normalized group key match")
	}
}
