package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestResolveSiteAccountProxyPrefersAccountProxy(t *testing.T) {
	accountProxyID := 2
	siteProxyID := 1

	selection := resolveSiteAccountProxy(&model.Site{
		ProxyMode:     model.ProxyUsageModePool,
		ProxyConfigID: &siteProxyID,
	}, &model.SiteAccount{
		ProxyMode:     model.ProxyUsageModePool,
		ProxyConfigID: &accountProxyID,
	})

	if selection.Mode != model.ProxyUsageModePool {
		t.Fatalf("expected account proxy mode pool, got %q", selection.Mode)
	}
	if selection.ConfigID == nil || *selection.ConfigID != accountProxyID {
		t.Fatalf("expected account proxy id %d, got %#v", accountProxyID, selection.ConfigID)
	}
}

func TestResolveSiteAccountProxyFallsBackToSiteSettings(t *testing.T) {
	siteProxyID := 1

	selection := resolveSiteAccountProxy(&model.Site{
		ProxyMode:     model.ProxyUsageModePool,
		ProxyConfigID: &siteProxyID,
	}, &model.SiteAccount{ProxyMode: model.ProxyUsageModeInherit})

	if selection.Mode != model.ProxyUsageModePool {
		t.Fatalf("expected site proxy mode pool, got %q", selection.Mode)
	}
	if selection.ConfigID == nil || *selection.ConfigID != siteProxyID {
		t.Fatalf("expected site proxy id %d, got %#v", siteProxyID, selection.ConfigID)
	}
}

func TestResolveSiteAccountProxyDisablesProxyWhenNoConfigExists(t *testing.T) {
	selection := resolveSiteAccountProxy(&model.Site{
		ProxyMode: model.ProxyUsageModeDirect,
	})

	if selection.Mode != model.ProxyUsageModeDirect {
		t.Fatalf("expected direct proxy mode, got %q", selection.Mode)
	}
	if selection.ConfigID != nil || selection.URL != "" {
		t.Fatalf("expected no proxy config, got %#v", selection)
	}
}

func TestResolveSiteAccountProxyInheritsSiteCustomURL(t *testing.T) {
	selection := resolveSiteAccountProxy(&model.Site{
		ProxyMode: model.ProxyUsageModeCustom,
		ProxyURL:  "http://127.0.0.1:7890",
	}, &model.SiteAccount{ProxyMode: model.ProxyUsageModeInherit})

	if selection.Mode != model.ProxyUsageModeCustom {
		t.Fatalf("expected site custom proxy mode, got %q", selection.Mode)
	}
	if selection.URL != "http://127.0.0.1:7890" {
		t.Fatalf("expected inherited custom proxy url, got %q", selection.URL)
	}
}

func TestResolveSiteAccountProxyAccountOverridesSiteCustomURL(t *testing.T) {
	accountProxyID := 3

	selection := resolveSiteAccountProxy(&model.Site{
		ProxyMode: model.ProxyUsageModeCustom,
		ProxyURL:  "http://127.0.0.1:7890",
	}, &model.SiteAccount{ProxyMode: model.ProxyUsageModePool, ProxyConfigID: &accountProxyID})

	if selection.Mode != model.ProxyUsageModePool || selection.ConfigID == nil || *selection.ConfigID != accountProxyID {
		t.Fatalf("expected account pool proxy %d, got %#v", accountProxyID, selection)
	}
	if selection.URL != "" {
		t.Fatalf("expected site custom url to be ignored, got %q", selection.URL)
	}
}

func TestBuildManagedAuthHeadersUsesCookieThenBearerFallback(t *testing.T) {
	headers := buildManagedAuthHeaders("sid=cookie-session")
	if len(headers) != 2 {
		t.Fatalf("expected two auth header candidates, got %d", len(headers))
	}
	if headers[0]["Cookie"] != "sid=cookie-session" {
		t.Fatalf("expected cookie header candidate first, got %#v", headers[0])
	}
	if headers[1]["Authorization"] != "Bearer sid=cookie-session" {
		t.Fatalf("expected bearer fallback candidate second, got %#v", headers[1])
	}
}

func TestBuildManagedAuthHeadersUsesBearerOnlyForPlainToken(t *testing.T) {
	headers := buildManagedAuthHeaders("plain-token")
	if len(headers) != 1 {
		t.Fatalf("expected one auth header candidate, got %d", len(headers))
	}
	if headers[0]["Authorization"] != "Bearer plain-token" {
		t.Fatalf("expected bearer header for plain token, got %#v", headers[0])
	}
}

func TestLooksLikeCookieToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "cookie-pair", token: "sid=cookie-session", want: true},
		{name: "cookie-chain", token: "sid=a; theme=dark", want: true},
		{name: "bearer-token", token: "Bearer plain-token", want: false},
		{name: "plain-token", token: "plain-token", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeCookieToken(tc.token); got != tc.want {
				t.Fatalf("looksLikeCookieToken(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}
