package xstrings

import "strings"

// SplitTrimCompact splits strings by sep, trims whitespace, and drops empty items.
// Commonly used for parsing comma-separated configs like "a, b, ,c,".
func SplitTrimCompact(sep string, parts ...string) []string {
	out := make([]string, 0)
	for _, p := range parts {
		if p == "" {
			continue
		}
		for _, item := range strings.Split(p, sep) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
	}
	return out
}

// TrimCompact trims whitespace and drops empty items in a string slice.
func TrimCompact(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// MatchWildcard 判断 value 是否匹配 pattern，`*` 匹配任意长度（含 0）的任意字符。
// 大小写不敏感，与仓库内其它模型名比较保持一致；`*` 可跨 `/`，
// 因此 `openai/*` 能命中 `openai/gpt-4o`（标准库 filepath.Match 不具备该语义）。
// pattern 为空时不匹配任何值；纯 `*` 匹配一切。
func MatchWildcard(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return strings.EqualFold(pattern, strings.TrimSpace(value))
	}
	return matchWildcardFold(strings.ToLower(pattern), strings.ToLower(strings.TrimSpace(value)))
}

// MatchAnyWildcard 判断 value 是否匹配逗号分隔模式串中的任意一个。
// patterns 为空或全为空白时不匹配任何值。
func MatchAnyWildcard(patterns, value string) bool {
	for _, pattern := range SplitTrimCompact(",", patterns) {
		if MatchWildcard(pattern, value) {
			return true
		}
	}
	return false
}

// matchWildcardFold 对已小写化的 pattern / value 做双指针 glob 匹配。
// star 记录最近一个 `*` 的位置，失配时回溯让该 `*` 多吞一个字符，
// 避免朴素递归在 `a*a*a*...` 这类模式上的指数级回溯。
func matchWildcardFold(pattern, value string) bool {
	var (
		p, v       int
		star       = -1
		vAfterStar int
	)
	for v < len(value) {
		switch {
		case p < len(pattern) && (pattern[p] == value[v] || pattern[p] == '*'):
			if pattern[p] == '*' {
				star = p
				vAfterStar = v
				p++
				continue
			}
			p++
			v++
		case star >= 0:
			p = star + 1
			vAfterStar++
			v = vAfterStar
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
