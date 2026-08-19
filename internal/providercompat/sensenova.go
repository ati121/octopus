package providercompat

import (
	"net/http"
	"net/url"
	"strings"
)

const senseNovaAPIHostname = "token.sensenova.cn"

// IsSenseNovaAPIURL reports whether rawURL targets SenseNova's official API
// hostname. Keep this exact-host based so lookalike domains do not silently
// change authentication semantics.
func IsSenseNovaAPIURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), senseNovaAPIHostname)
}

// SetAnthropicAuthHeader applies the authentication scheme expected by an
// Anthropic-compatible upstream. Anthropic uses x-api-key, while SenseNova's
// official compatibility endpoint reuses its OpenAI-style Bearer token.
//
// SenseNova reference: https://platform.sensenova.cn/docs
func SetAnthropicAuthHeader(header http.Header, baseURL, key string) {
	if header == nil {
		return
	}
	if IsSenseNovaAPIURL(baseURL) {
		header.Del("X-API-Key")
		header.Set("Authorization", "Bearer "+key)
		return
	}
	header.Set("X-API-Key", key)
}
