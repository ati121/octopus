package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestEnsurePublicGroupsForSiteAccountCreatesNormalizedGroups(t *testing.T) {
	setupAutoGroupTestDB(t)
	context := t.Context()
	if err := SettingSetString(model.SettingKeyAutoGroupNormalizeEnabled, "true"); err != nil {
		t.Fatalf("enable normalization: %v", err)
	}
	site := &model.Site{Name: "site", BaseURL: "https://example.com", Platform: model.SitePlatformNewAPI, Enabled: true}
	if err := SiteCreate(site, context); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{SiteID: site.ID, Name: "account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true}
	if err := SiteAccountCreate(account, context); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	channel := &model.Channel{
		Name:     "projected",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com"}},
		Model:    "gpt-4o-2024-08-06,openai/gpt-4o-mini",
		Keys:     []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
	if err := ChannelCreate(channel, context); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	binding := model.SiteChannelBinding{SiteID: site.ID, SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ChannelID: channel.ID}
	if err := dbpkg.GetDB().WithContext(context).Create(&binding).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}
	result, err := EnsurePublicGroupsForSiteAccount(site.ID, account.ID, context)
	if err != nil {
		t.Fatalf("EnsurePublicGroupsForSiteAccount failed: %v", err)
	}
	if result.ChannelsProcessed != 1 || result.GroupsCreated < 1 {
		t.Fatalf("unexpected ensure result: %+v", result)
	}
	groups, err := GroupList(context)
	if err != nil {
		t.Fatalf("GroupList failed: %v", err)
	}
	names := make(map[string]bool, len(groups))
	for _, group := range groups {
		names[group.Name] = true
	}
	if !names["gpt-4o"] || !names["gpt-4o-mini"] {
		t.Fatalf("expected normalized public groups, got %v", names)
	}
}
