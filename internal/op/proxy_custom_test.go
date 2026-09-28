package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelCustomProxyURLPersistsAndClears(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:      "custom-proxy-channel",
		Type:      outbound.OutboundTypeOpenAIChat,
		Enabled:   true,
		ProxyMode: model.ProxyUsageModeCustom,
		ProxyURL:  " HTTP://127.0.0.1:7890 ",
		Keys:      []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if got := channelRowInDB(t, ctx, channel.ID).ProxyURL; got != "http://127.0.0.1:7890" {
		t.Fatalf("expected normalized proxy url to be persisted, got %q", got)
	}
	cached, err := ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if cached.ProxyMode != model.ProxyUsageModeCustom || cached.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("expected cached channel to keep custom proxy, got mode=%q url=%q", cached.ProxyMode, cached.ProxyURL)
	}

	socks := "socks5://user:pass@127.0.0.1:1080"
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, ProxyURL: &socks}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}
	if updated.ProxyURL != socks || channelRowInDB(t, ctx, channel.ID).ProxyURL != socks {
		t.Fatalf("expected proxy url to be replaced, got %q", updated.ProxyURL)
	}

	empty := ""
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, ProxyURL: &empty}, ctx); err == nil {
		t.Fatal("expected clearing the url while staying in custom mode to be rejected")
	}
	if got := channelRowInDB(t, ctx, channel.ID).ProxyURL; got != socks {
		t.Fatalf("rejected update must not change the stored url, got %q", got)
	}

	direct := model.ProxyUsageModeDirect
	updated, err = ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, ProxyMode: &direct}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate failed: %v", err)
	}
	if updated.ProxyURL != "" || channelRowInDB(t, ctx, channel.ID).ProxyURL != "" {
		t.Fatalf("expected proxy url to be cleared after switching to direct, got %q", updated.ProxyURL)
	}
}

func TestChannelCreateRejectsCustomProxyWithoutURL(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:      "custom-proxy-missing-url",
		Type:      outbound.OutboundTypeOpenAIChat,
		Enabled:   true,
		ProxyMode: model.ProxyUsageModeCustom,
		Keys:      []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err == nil {
		t.Fatal("expected custom proxy without url to be rejected")
	}
}

func TestSiteCustomProxyURLPersistsAndClears(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:      "custom-proxy-site",
		Platform:  model.SitePlatformNewAPI,
		BaseURL:   "https://example.com",
		Enabled:   true,
		ProxyMode: model.ProxyUsageModeCustom,
		ProxyURL:  "http://127.0.0.1:7890",
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	reloaded, err := SiteGet(site.ID, ctx)
	if err != nil {
		t.Fatalf("SiteGet failed: %v", err)
	}
	if reloaded.ProxyMode != model.ProxyUsageModeCustom || reloaded.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("expected site custom proxy to be kept, got mode=%q url=%q", reloaded.ProxyMode, reloaded.ProxyURL)
	}

	socks := "SOCKS5://127.0.0.1:1080"
	updated, err := SiteUpdate(&model.SiteUpdateRequest{ID: site.ID, ProxyURL: &socks}, ctx)
	if err != nil {
		t.Fatalf("SiteUpdate failed: %v", err)
	}
	if updated.ProxyURL != "socks5://127.0.0.1:1080" {
		t.Fatalf("expected normalized proxy url, got %q", updated.ProxyURL)
	}

	system := model.ProxyUsageModeSystem
	updated, err = SiteUpdate(&model.SiteUpdateRequest{ID: site.ID, ProxyMode: &system}, ctx)
	if err != nil {
		t.Fatalf("SiteUpdate failed: %v", err)
	}
	if updated.ProxyURL != "" {
		t.Fatalf("expected proxy url to be cleared after switching to system, got %q", updated.ProxyURL)
	}
	var row model.Site
	if err := dbpkg.GetDB().Where("id = ?", site.ID).First(&row).Error; err != nil {
		t.Fatalf("query site failed: %v", err)
	}
	if row.ProxyURL != "" {
		t.Fatalf("expected persisted proxy url to be cleared, got %q", row.ProxyURL)
	}

	custom := model.ProxyUsageModeCustom
	if _, err := SiteUpdate(&model.SiteUpdateRequest{ID: site.ID, ProxyMode: &custom}, ctx); err == nil {
		t.Fatal("expected switching to custom without url to be rejected")
	}
}
