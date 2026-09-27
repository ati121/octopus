package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestChannelLLMListReportsPerModelChannelType(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	channel := &model.Channel{
		Name:       "llm-list-model-types",
		Type:       outbound.OutboundTypeOpenAIChat,
		Enabled:    true,
		Model:      "gpt-4o,deepseek-v4.1-flash",
		ModelTypes: model.ChannelModelTypes{"deepseek-v4.1-flash": outbound.OutboundTypeOpenAIResponse},
		Keys:       []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	models, err := ChannelLLMList(ctx)
	if err != nil {
		t.Fatalf("ChannelLLMList failed: %v", err)
	}
	got := make(map[string]outbound.OutboundType, len(models))
	for _, item := range models {
		got[item.Name] = item.ChannelType
	}
	if len(got) != 2 {
		t.Fatalf("expected both models to be listed, got %+v", models)
	}
	if got["deepseek-v4.1-flash"] != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("expected per-model type to win, got %d", got["deepseek-v4.1-flash"])
	}
	if got["gpt-4o"] != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected model without its own type to follow the channel, got %d", got["gpt-4o"])
	}
}

// 按协议拆分的站点渠道，绑定键形如 default::openai-response；列表里的分组必须是 default，
// 不能把协议后缀带进分组名。
func TestChannelLLMListStripsRouteSuffixFromSiteGroup(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{Name: "feng", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "7up",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	channel := &model.Channel{
		Name:    "feng/7up/default-Response",
		Type:    outbound.OutboundTypeOpenAIResponse,
		Enabled: true,
		Model:   "gpt-5.4",
		Keys:    []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	binding := model.SiteChannelBinding{
		SiteID:        site.ID,
		SiteAccountID: account.ID,
		GroupKey:      model.ComposeSiteChannelBindingKey(model.SiteDefaultGroupKey, model.SiteModelRouteTypeOpenAIResponse, true),
		ChannelID:     channel.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}

	models, err := ChannelLLMList(ctx)
	if err != nil {
		t.Fatalf("ChannelLLMList failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected the managed model to be listed, got %+v", models)
	}
	item := models[0]
	if item.SiteGroupKey != model.SiteDefaultGroupKey || item.SiteGroupName != model.SiteDefaultGroupName {
		t.Fatalf("expected base group default, got key=%q name=%q", item.SiteGroupKey, item.SiteGroupName)
	}
	if item.SiteName != "feng" || item.SiteAccountName != "7up" {
		t.Fatalf("unexpected site metadata: site=%q account=%q", item.SiteName, item.SiteAccountName)
	}
	if item.ChannelType != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("expected managed channel type Response, got %d", item.ChannelType)
	}
}
