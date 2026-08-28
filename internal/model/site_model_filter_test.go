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

func TestApplySiteModelDisabledStateRecomputesFromFilter(t *testing.T) {
	models := []SiteModel{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "claude-opus-5", Disabled: true},
		{SiteAccountID: 1, GroupKey: "default", ModelName: "deepseek-v4"},
		{SiteAccountID: 1, GroupKey: "vip", ModelName: "gpt-5", Disabled: true},
	}
	changed := ApplySiteModelDisabledState(models, map[string]string{"default": "^claude-"}, nil)
	if changed != 3 {
		t.Fatalf("changed = %d, want 3", changed)
	}
	if models[0].Disabled {
		t.Fatalf("matched model must be enabled")
	}
	if !models[1].Disabled {
		t.Fatalf("unmatched model must be disabled")
	}
	// vip 既没有正则也没有用户表态：Disabled 是派生列，不承载记忆，必须回到启用。
	// 手动停用的记忆由 SiteModelStateOverride 承担。
	if models[2].Disabled {
		t.Fatalf("without a filter or an override the model must be enabled")
	}
}

// 用户表态是逐个例外，正则只是批量默认值，因此表态必须压过正则的两个方向。
func TestApplySiteModelDisabledStateOverrideBeatsFilter(t *testing.T) {
	models := []SiteModel{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "claude-opus-5"},
		{SiteAccountID: 1, GroupKey: "default", ModelName: "deepseek-v4", Disabled: true},
	}
	overrides := NewSiteModelDisabledOverrides([]SiteModelStateOverride{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "claude-opus-5", Disabled: true},
		{SiteAccountID: 1, GroupKey: "default", ModelName: "deepseek-v4", Disabled: false},
	})
	if changed := ApplySiteModelDisabledState(models, map[string]string{"default": "^claude-"}, overrides); changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	if !models[0].Disabled {
		t.Fatalf("manual disable must survive a matching filter")
	}
	if models[1].Disabled {
		t.Fatalf("manual enable must survive a non-matching filter")
	}
}

// 表态只对本账号本分组的同名模型生效，不能跨账号串味。
func TestApplySiteModelDisabledStateScopesOverrideToAccountAndGroup(t *testing.T) {
	models := []SiteModel{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-5"},
		{SiteAccountID: 2, GroupKey: "default", ModelName: "gpt-5"},
		{SiteAccountID: 1, GroupKey: "vip", ModelName: "gpt-5"},
	}
	overrides := NewSiteModelDisabledOverrides([]SiteModelStateOverride{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-5", Disabled: true},
	})
	if changed := ApplySiteModelDisabledState(models, nil, overrides); changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if !models[0].Disabled {
		t.Fatalf("override must apply to its own account and group")
	}
	if models[1].Disabled || models[2].Disabled {
		t.Fatalf("override must not leak to another account or group")
	}
}

// 非法正则视为「未配置」：宁可少停用，也不要让脏数据把整组模型误杀。
func TestApplySiteModelDisabledStateIgnoresBlankAndInvalidPatterns(t *testing.T) {
	models := []SiteModel{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "claude-opus-5", Disabled: true},
		{SiteAccountID: 1, GroupKey: "vip", ModelName: "gpt-5", Disabled: true},
	}
	changed := ApplySiteModelDisabledState(models, map[string]string{
		"default": "",
		"vip":     "^(gpt",
	}, nil)
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	for i := range models {
		if models[i].Disabled {
			t.Fatalf("model %q must be enabled when its group has no usable filter", models[i].ModelName)
		}
	}
}

func TestApplySiteModelDisabledStateNormalizesGroupKey(t *testing.T) {
	models := []SiteModel{{SiteAccountID: 1, GroupKey: "", ModelName: "claude-opus-5", Disabled: true}}
	if changed := ApplySiteModelDisabledState(models, map[string]string{"": "^claude-"}, nil); changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if models[0].Disabled {
		t.Fatalf("expected model to be enabled after normalized group key match")
	}
}
