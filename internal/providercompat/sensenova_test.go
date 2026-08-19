package providercompat

import (
	"net/http"
	"testing"
)

func TestIsSenseNovaAPIURL(t *testing.T) {
	tests := map[string]bool{
		"https://token.sensenova.cn":                   true,
		"https://TOKEN.SENSENOVA.CN/v1/":               true,
		"https://token.sensenova.cn:443/v1/messages":   true,
		"https://token.sensenova.cn.evil.example/v1":   false,
		"https://proxy.example.com/token.sensenova.cn": false,
		"token.sensenova.cn/v1":                        false,
		"https://api.anthropic.com/v1":                 false,
		"":                                             false,
	}
	for rawURL, want := range tests {
		t.Run(rawURL, func(t *testing.T) {
			if got := IsSenseNovaAPIURL(rawURL); got != want {
				t.Fatalf("IsSenseNovaAPIURL(%q) = %t, want %t", rawURL, got, want)
			}
		})
	}
}

func TestSetAnthropicAuthHeader(t *testing.T) {
	t.Run("sensenova bearer", func(t *testing.T) {
		header := make(http.Header)
		header.Set("X-API-Key", "stale-key")
		SetAnthropicAuthHeader(header, "https://token.sensenova.cn/v1", "sense-key")
		if got := header.Get("Authorization"); got != "Bearer sense-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := header.Get("X-API-Key"); got != "" {
			t.Fatalf("X-API-Key should be removed, got %q", got)
		}
	})

	t.Run("standard anthropic api key", func(t *testing.T) {
		header := make(http.Header)
		SetAnthropicAuthHeader(header, "https://api.anthropic.com/v1", "anthropic-key")
		if got := header.Get("X-API-Key"); got != "anthropic-key" {
			t.Fatalf("X-API-Key = %q", got)
		}
		if got := header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization should be empty, got %q", got)
		}
	})
}
