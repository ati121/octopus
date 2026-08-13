package xstrings

import "testing"

func TestMatchWildcard(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"精确匹配", "gpt-4o", "gpt-4o", true},
		{"精确不匹配", "gpt-4o", "gpt-4o-mini", false},
		{"大小写不敏感", "GPT-4O", "gpt-4o", true},
		{"前缀通配", "gpt-4*", "gpt-4o-mini", true},
		{"前缀通配不命中", "gpt-4*", "gpt-3.5-turbo", false},
		{"后缀通配", "*-mini", "gpt-4o-mini", true},
		{"中间通配", "gpt*mini", "gpt-4o-mini", true},
		{"通配符匹配空串", "gpt-4o*", "gpt-4o", true},
		{"纯通配匹配一切", "*", "anything", true},
		{"通配符跨斜杠", "openai/*", "openai/gpt-4o", true},
		{"通配符跨多级斜杠", "*/gpt-4o", "vendor/openai/gpt-4o", true},
		{"多个通配符", "*gpt*4o*", "azure-gpt-4o-preview", true},
		{"多个通配符不命中", "*gpt*4o*", "claude-opus", false},
		{"空模式不匹配", "", "gpt-4o", false},
		{"空模式空值也不匹配", "", "", false},
		{"模式两端空白被忽略", "  gpt-4o  ", "gpt-4o", true},
		{"值两端空白被忽略", "gpt-4o", "  gpt-4o  ", true},
		{"通配符匹配空值", "*", "", true},
		{"非通配模式不匹配空值", "gpt-4o", "", false},
		{"回溯：重复字符", "a*a*a", "aaaa", true},
		{"回溯：重复字符不足", "a*a*a", "aa", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchWildcard(tc.pattern, tc.value); got != tc.want {
				t.Fatalf("MatchWildcard(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
			}
		})
	}
}

func TestMatchAnyWildcard(t *testing.T) {
	cases := []struct {
		name     string
		patterns string
		value    string
		want     bool
	}{
		{"命中第一个", "gpt-4*,o3-*", "gpt-4o", true},
		{"命中第二个", "gpt-4*,o3-*", "o3-mini", true},
		{"都不命中", "gpt-4*,o3-*", "claude-sonnet-4", false},
		{"空串不匹配", "", "gpt-4o", false},
		{"全空白不匹配", " , , ", "gpt-4o", false},
		{"忽略空项与空白", " gpt-4* , ,o3-* ", "o3-mini", true},
		{"单个精确项", "gpt-4o", "gpt-4o", true},
		{"含纯通配项匹配一切", "gpt-4*,*", "claude-opus", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchAnyWildcard(tc.patterns, tc.value); got != tc.want {
				t.Fatalf("MatchAnyWildcard(%q, %q) = %v, want %v", tc.patterns, tc.value, got, tc.want)
			}
		})
	}
}
