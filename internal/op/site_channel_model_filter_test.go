package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupModelFilterFixture(t *testing.T) (context.Context, *model.SiteAccount) {
	t.Helper()
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-model-filter",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-model-filter-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	rows := []model.SiteModel{
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "claude-opus-5", RouteType: model.SiteModelRouteTypeAnthropic, RouteSource: model.SiteModelRouteSourceSyncInferred, Disabled: true},
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4.1", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred},
		{SiteAccountID: account.ID, GroupKey: "vip", ModelName: "gpt-4.1", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}
	return ctx, account
}

func modelDisabledByName(t *testing.T, ctx context.Context, accountID int, groupKey string) map[string]bool {
	t.Helper()
	var rows []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", accountID, groupKey).Find(&rows).Error; err != nil {
		t.Fatalf("query site models failed: %v", err)
	}
	result := make(map[string]bool, len(rows))
	for _, row := range rows {
		result[row.ModelName] = row.Disabled
	}
	return result
}

func TestUpdateSiteGroupModelFilterRecomputesOnlyTargetGroup(t *testing.T) {
	ctx, account := setupModelFilterFixture(t)

	req := &model.SiteGroupModelFilterUpdateRequest{
		GroupKey:         model.SiteDefaultGroupKey,
		ModelFilterRegex: "^claude-",
	}
	if err := UpdateSiteGroupModelFilter(account.SiteID, account.ID, req, ctx); err != nil {
		t.Fatalf("UpdateSiteGroupModelFilter failed: %v", err)
	}

	var group model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).First(&group).Error; err != nil {
		t.Fatalf("query group failed: %v", err)
	}
	if group.ModelFilterRegex != "^claude-" {
		t.Fatalf("expected regex to be persisted, got %q", group.ModelFilterRegex)
	}

	defaultGroup := modelDisabledByName(t, ctx, account.ID, model.SiteDefaultGroupKey)
	if defaultGroup["claude-opus-5"] {
		t.Fatalf("matched model must be enabled")
	}
	if !defaultGroup["gpt-4.1"] {
		t.Fatalf("unmatched model must be disabled")
	}

	// 正则按分组独立生效，其它分组不能被连带修改。
	vipGroup := modelDisabledByName(t, ctx, account.ID, "vip")
	if vipGroup["gpt-4.1"] {
		t.Fatalf("other groups must not be touched")
	}
}

func TestUpdateSiteGroupModelFilterClearingEnablesAll(t *testing.T) {
	ctx, account := setupModelFilterFixture(t)

	if err := UpdateSiteGroupModelFilter(account.SiteID, account.ID, &model.SiteGroupModelFilterUpdateRequest{
		GroupKey:         model.SiteDefaultGroupKey,
		ModelFilterRegex: "^claude-",
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteGroupModelFilter failed: %v", err)
	}
	if err := UpdateSiteGroupModelFilter(account.SiteID, account.ID, &model.SiteGroupModelFilterUpdateRequest{
		GroupKey:         model.SiteDefaultGroupKey,
		ModelFilterRegex: "  ",
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteGroupModelFilter clear failed: %v", err)
	}

	var group model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).First(&group).Error; err != nil {
		t.Fatalf("query group failed: %v", err)
	}
	if group.ModelFilterRegex != "" {
		t.Fatalf("expected regex to be cleared, got %q", group.ModelFilterRegex)
	}

	for name, disabled := range modelDisabledByName(t, ctx, account.ID, model.SiteDefaultGroupKey) {
		if disabled {
			t.Fatalf("clearing the regex must enable every model in the group, %q is still disabled", name)
		}
	}
}

func TestUpdateSiteGroupModelFilterRejectsInvalidRegex(t *testing.T) {
	ctx, account := setupModelFilterFixture(t)

	err := UpdateSiteGroupModelFilter(account.SiteID, account.ID, &model.SiteGroupModelFilterUpdateRequest{
		GroupKey:         model.SiteDefaultGroupKey,
		ModelFilterRegex: "^(claude",
	}, ctx)
	if err == nil {
		t.Fatalf("expected an error for an invalid regex")
	}

	// 校验失败不能留下任何副作用。
	before := modelDisabledByName(t, ctx, account.ID, model.SiteDefaultGroupKey)
	if !before["claude-opus-5"] || before["gpt-4.1"] {
		t.Fatalf("rejected request must not change model state, got %+v", before)
	}
}
