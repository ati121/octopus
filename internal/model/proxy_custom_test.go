package model

import (
	"strings"
	"testing"
)

func TestNormalizeCustomProxyURL(t *testing.T) {
	if got, err := NormalizeCustomProxyURL(ProxyUsageModeDirect, "http://127.0.0.1:7890"); err != nil || got != "" {
		t.Fatalf("non-custom mode must drop the url, got %q err=%v", got, err)
	}
	if _, err := NormalizeCustomProxyURL(ProxyUsageModeCustom, "  "); err == nil {
		t.Fatal("expected empty custom proxy url to be rejected")
	}
	if _, err := NormalizeCustomProxyURL(ProxyUsageModeCustom, "ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected unsupported scheme to be rejected")
	}
	got, err := NormalizeCustomProxyURL(ProxyUsageModeCustom, " SOCKS5://user:pass@LOCALHOST:1080 ")
	if err != nil {
		t.Fatalf("NormalizeCustomProxyURL failed: %v", err)
	}
	if got != "socks5://user:pass@localhost:1080" {
		t.Fatalf("expected normalized url, got %q", got)
	}
}

func TestSiteValidateCustomProxyURL(t *testing.T) {
	site := &Site{Name: "s", Platform: SitePlatformNewAPI, BaseURL: "https://example.com", ProxyMode: ProxyUsageModeCustom}
	if err := site.Validate(); err == nil {
		t.Fatal("expected custom proxy mode without url to be rejected")
	}

	site.ProxyURL = " HTTP://127.0.0.1:7890 "
	if err := site.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if site.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("expected normalized proxy url, got %q", site.ProxyURL)
	}

	site.ProxyMode = ProxyUsageModeSystem
	if err := site.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if site.ProxyURL != "" {
		t.Fatalf("expected proxy url to be cleared outside custom mode, got %q", site.ProxyURL)
	}
}

func TestSiteAccountValidateRejectsCustomProxy(t *testing.T) {
	account := &SiteAccount{SiteID: 1, Name: "a", CredentialType: SiteCredentialTypeAccessToken, ProxyMode: ProxyUsageModeCustom}
	if err := account.Validate(); err == nil || !strings.Contains(err.Error(), "custom") {
		t.Fatalf("expected site account custom proxy mode to be rejected, got %v", err)
	}
}
