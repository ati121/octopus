package op

import (
	"regexp"
	"strings"
)

var autoGroupProviderPrefixes = map[string]struct{}{
	"openai": {}, "anthropic": {}, "google": {}, "gemini": {}, "meta": {},
	"meta-llama": {}, "mistral": {}, "mistralai": {}, "deepseek": {},
	"qwen": {}, "alibaba": {}, "dashscope": {}, "zhipu": {}, "zhihu": {},
	"cohere": {}, "groq": {}, "xai": {}, "perplexity": {}, "together": {},
	"fireworks": {}, "openrouter": {}, "azure": {}, "aws": {}, "bedrock": {},
	"vertex": {}, "vertex_ai": {}, "huggingface": {}, "moonshot": {},
	"minimax": {}, "baichuan": {}, "yi": {}, "01-ai": {}, "lingyiwanwu": {},
}

var (
	reISODateSuffix   = regexp.MustCompile(`(?i)-(\d{4})-(\d{2})-(\d{2})$`)
	reCompactDateSuf  = regexp.MustCompile(`(?i)-(\d{8})$`)
	reSnapshotDateSuf = regexp.MustCompile(`(?i)-(\d{4})-(\d{2})-(\d{2})-(preview|latest|exp|experimental|beta|alpha)$`)
)

// NormalizePublicModelName derives a stable public model/group name from an
// upstream identifier without changing the original channel model id.
func NormalizePublicModelName(name string) string {
	value := strings.TrimSpace(name)
	if value == "" {
		return ""
	}
	value = stripProviderPrefix(value)
	value = stripTrailingDateSuffix(value)
	return strings.TrimSpace(value)
}

func PublicModelNamesMatch(modelName, groupName string, normalize bool) bool {
	modelName = strings.TrimSpace(modelName)
	groupName = strings.TrimSpace(groupName)
	if modelName == "" || groupName == "" {
		return false
	}
	if strings.EqualFold(modelName, groupName) {
		return true
	}
	if !normalize {
		return false
	}
	left := NormalizePublicModelName(modelName)
	right := NormalizePublicModelName(groupName)
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func stripProviderPrefix(value string) string {
	separator := -1
	for index, r := range value {
		if r == '/' || r == ':' {
			separator = index
			break
		}
	}
	if separator <= 0 || separator >= len(value)-1 {
		return value
	}
	prefix := strings.ToLower(strings.TrimSpace(value[:separator]))
	if _, ok := autoGroupProviderPrefixes[prefix]; !ok {
		return value
	}
	return strings.TrimSpace(value[separator+1:])
}

func stripTrailingDateSuffix(value string) string {
	if location := reSnapshotDateSuf.FindStringIndex(value); location != nil {
		return value[:location[0]]
	}
	if location := reISODateSuffix.FindStringIndex(value); location != nil {
		return value[:location[0]]
	}
	if location := reCompactDateSuf.FindStringIndex(value); location != nil {
		return value[:location[0]]
	}
	return value
}
