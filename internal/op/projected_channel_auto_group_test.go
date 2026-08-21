package op

import (
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func setupAutoGroupTestDB(t *testing.T) {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "auto-group.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	groupCache.Clear()
	groupMap.Clear()
	channelCache.Clear()
	settingCache.Clear()
	if err := settingRefreshCache(t.Context()); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
}

func autoGroupTestChannel(name, models string, autoGroup model.AutoGroupType) *model.Channel {
	return &model.Channel{
		Name:      name,
		Type:      outbound.OutboundTypeOpenAIChat,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: "https://example.com"}},
		Model:     models,
		AutoGroup: autoGroup,
		Keys:      []model.ChannelKey{{ChannelKey: "sk-test", Enabled: true}},
	}
}

func autoGroupModels(t *testing.T, groupID, channelID int) []string {
	t.Helper()
	group, err := GroupGet(groupID, t.Context())
	if err != nil {
		t.Fatalf("GroupGet failed: %v", err)
	}
	models := make([]string, 0)
	for _, item := range group.Items {
		if item.ChannelID == channelID {
			models = append(models, item.ModelName)
		}
	}
	return models
}

func containsAllModels(models []string, expected ...string) bool {
	set := make(map[string]struct{}, len(models))
	for _, modelName := range models {
		set[modelName] = struct{}{}
	}
	for _, modelName := range expected {
		if _, exists := set[modelName]; !exists {
			return false
		}
	}
	return true
}

func TestChannelEnabledAppendsExistingGroupItemsToTail(t *testing.T) {
	setupAutoGroupTestDB(t)
	ctx := t.Context()
	first := autoGroupTestChannel("first", "model-a,model-b", model.AutoGroupTypeNone)
	second := autoGroupTestChannel("second", "model-c", model.AutoGroupTypeNone)
	hidden := autoGroupTestChannel("hidden", "model-d", model.AutoGroupTypeNone)
	if err := ChannelCreate(first, ctx); err != nil {
		t.Fatalf("ChannelCreate(first) failed: %v", err)
	}
	if err := ChannelCreate(second, ctx); err != nil {
		t.Fatalf("ChannelCreate(second) failed: %v", err)
	}
	if err := ChannelCreate(hidden, ctx); err != nil {
		t.Fatalf("ChannelCreate(hidden) failed: %v", err)
	}
	group := &model.Group{
		Name: "tail-order",
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: first.ID, ModelName: "model-a", Priority: 1, Weight: 1},
			{ChannelID: first.ID, ModelName: "model-b", Priority: 2, Weight: 1},
			{ChannelID: second.ID, ModelName: "model-c", Priority: 3, Weight: 1},
			{ChannelID: hidden.ID, ModelName: "model-d", Priority: 10, Weight: 1},
		},
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := ChannelEnabled(first.ID, false, ctx); err != nil {
		t.Fatalf("ChannelEnabled(false) failed: %v", err)
	}
	if err := ChannelEnabled(hidden.ID, false, ctx); err != nil {
		t.Fatalf("ChannelEnabled(hidden, false) failed: %v", err)
	}
	if err := ChannelEnabled(first.ID, true, ctx); err != nil {
		t.Fatalf("ChannelEnabled(true) failed: %v", err)
	}

	reloaded, err := GroupGet(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupGet failed: %v", err)
	}
	items := append([]model.GroupItem(nil), reloaded.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Priority < items[j].Priority })
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.ModelName)
	}
	want := []string{"model-c", "model-a", "model-b", "model-d"}
	if len(got) != len(want) {
		t.Fatalf("group item count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("group item order = %v, want %v", got, want)
		}
	}
	if items[1].Priority != 4 || items[2].Priority != 5 {
		t.Fatalf("re-enabled priorities = %d,%d, want 4,5", items[1].Priority, items[2].Priority)
	}
}

func TestManagedChannelListsHideDisabledSiteImmediately(t *testing.T) {
	setupAutoGroupTestDB(t)
	ctx := t.Context()
	site := &model.Site{
		Name:     "disabled-source-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	channel := autoGroupTestChannel("managed", "hidden-model", model.AutoGroupTypeNone)
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	binding := model.SiteChannelBinding{
		SiteID:        site.ID,
		SiteAccountID: account.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		ChannelID:     channel.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}
	group := &model.Group{
		Name:  "hidden-model",
		Mode:  model.GroupModeFailover,
		Items: []model.GroupItem{{ChannelID: channel.ID, ModelName: "hidden-model", Priority: 1, Weight: 1}},
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}

	if err := SiteEnabled(site.ID, false, ctx); err != nil {
		t.Fatalf("SiteEnabled(false) failed: %v", err)
	}
	if cached, err := ChannelGet(channel.ID, ctx); err != nil || !cached.Enabled {
		t.Fatalf("expected cached channel to still be enabled before projection, got channel=%+v err=%v", cached, err)
	}
	models, err := ChannelLLMList(ctx)
	if err != nil {
		t.Fatalf("ChannelLLMList failed: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("disabled site models should be hidden, got %+v", models)
	}
	groups, err := GroupListAvailable(ctx)
	if err != nil {
		t.Fatalf("GroupListAvailable failed: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Items) != 0 {
		t.Fatalf("disabled site group items should be hidden, got %+v", groups)
	}
	routable, err := GroupGetEnabledMap(group.Name, ctx)
	if err != nil {
		t.Fatalf("GroupGetEnabledMap failed: %v", err)
	}
	if len(routable.Items) != 0 {
		t.Fatalf("disabled site group items should not be routable, got %+v", routable.Items)
	}
}

func TestChannelAutoGroupRegexPrunesNonMatchingModels(t *testing.T) {
	setupAutoGroupTestDB(t)
	context := t.Context()
	channel := autoGroupTestChannel("regex", "gpt-5.4,gpt-5.4-mini,gpt-5.4-nano", model.AutoGroupTypeRegex)
	if err := ChannelCreate(channel, context); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: "gpt-5.4", MatchRegex: `^gpt-5\.4$`, Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, context); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := GroupItemBatchAdd(group.ID, []model.GroupIDAndLLMName{
		{ChannelID: channel.ID, ModelName: "gpt-5.4"},
		{ChannelID: channel.ID, ModelName: "gpt-5.4-mini"},
		{ChannelID: channel.ID, ModelName: "gpt-5.4-nano"},
	}, context); err != nil {
		t.Fatalf("seed group items failed: %v", err)
	}
	reloaded, _ := ChannelGet(channel.ID, context)
	ChannelAutoGroupWithMode(reloaded, model.AutoGroupTypeRegex, context)
	models := autoGroupModels(t, group.ID, channel.ID)
	if len(models) != 1 || !containsAllModels(models, "gpt-5.4") {
		t.Fatalf("expected stale regex mappings to be removed, got %v", models)
	}
}

func TestChannelAutoGroupDoesNotTouchOtherChannels(t *testing.T) {
	setupAutoGroupTestDB(t)
	context := t.Context()
	first := autoGroupTestChannel("first", "gpt-5.4-mini", model.AutoGroupTypeRegex)
	second := autoGroupTestChannel("second", "gpt-5.4", model.AutoGroupTypeRegex)
	if err := ChannelCreate(first, context); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	if err := ChannelCreate(second, context); err != nil {
		t.Fatalf("create second channel: %v", err)
	}
	group := &model.Group{Name: "gpt-5.4", MatchRegex: `^gpt-5\.4$`, Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, context); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := GroupItemBatchAdd(group.ID, []model.GroupIDAndLLMName{
		{ChannelID: first.ID, ModelName: "gpt-5.4-mini"},
		{ChannelID: second.ID, ModelName: "gpt-5.4"},
	}, context); err != nil {
		t.Fatalf("seed group items failed: %v", err)
	}
	reloaded, _ := ChannelGet(second.ID, context)
	ChannelAutoGroupWithMode(reloaded, model.AutoGroupTypeRegex, context)
	if models := autoGroupModels(t, group.ID, first.ID); len(models) != 1 || !containsAllModels(models, "gpt-5.4-mini") {
		t.Fatalf("other channel mappings changed unexpectedly: %v", models)
	}
}

func TestChannelAutoGroupCreateMissingWithNormalizeUsesPublicName(t *testing.T) {
	setupAutoGroupTestDB(t)
	context := t.Context()
	if err := SettingSetString(model.SettingKeyAutoGroupCreateMissingEnabled, "true"); err != nil {
		t.Fatalf("enable create missing: %v", err)
	}
	if err := SettingSetString(model.SettingKeyAutoGroupNormalizeEnabled, "true"); err != nil {
		t.Fatalf("enable normalization: %v", err)
	}
	channel := autoGroupTestChannel("normalized", "gpt-4o-2024-08-06,openai/gpt-4o", model.AutoGroupTypeExact)
	if err := ChannelCreate(channel, context); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	reloaded, _ := ChannelGet(channel.ID, context)
	ChannelAutoGroupWithMode(reloaded, model.AutoGroupTypeExact, context)
	groups, err := GroupList(context)
	if err != nil {
		t.Fatalf("GroupList failed: %v", err)
	}
	if len(groups) != 1 || groups[0].Name != "gpt-4o" {
		t.Fatalf("expected one normalized group, got %+v", groups)
	}
	models := autoGroupModels(t, groups[0].ID, channel.ID)
	if len(models) != 2 || !containsAllModels(models, "gpt-4o-2024-08-06", "openai/gpt-4o") {
		t.Fatalf("expected both upstream model ids, got %v", models)
	}
}

func TestDefaultChannelAutoGroupFollowsGlobalMode(t *testing.T) {
	setupAutoGroupTestDB(t)

	// 新建渠道未指定模式时落「跟随全局」，而不是把当时的全局模式快照下来。
	if got := DefaultChannelAutoGroup(model.AutoGroupTypeNone); got != model.AutoGroupTypeInherit {
		t.Fatalf("expected inherit for unspecified mode, got %d", got)
	}
	// 显式指定的模式不被全局覆盖。
	if got := DefaultChannelAutoGroup(model.AutoGroupTypeFuzzy); got != model.AutoGroupTypeFuzzy {
		t.Fatalf("expected explicit fuzzy mode to survive, got %d", got)
	}

	// 全局未开启时，跟随全局解析为「不自动分组」。
	if got := ResolveChannelAutoGroup(model.AutoGroupTypeInherit, false); got != model.AutoGroupTypeNone {
		t.Fatalf("expected none without global mode, got %d", got)
	}

	if err := SettingSetString(model.SettingKeyProjectedChannelAutoGroupEnabled, strconv.Itoa(int(model.AutoGroupTypeRegex))); err != nil {
		t.Fatalf("set global auto group mode: %v", err)
	}
	// 全局改了之后，跟随全局的渠道立刻跟着变。
	if got := ResolveChannelAutoGroup(model.AutoGroupTypeInherit, false); got != model.AutoGroupTypeRegex {
		t.Fatalf("expected inherit channel to follow global regex mode, got %d", got)
	}
	// 单独设置过的渠道不受全局影响。
	if got := ResolveChannelAutoGroup(model.AutoGroupTypeExact, false); got != model.AutoGroupTypeExact {
		t.Fatalf("expected explicit exact mode to survive global change, got %d", got)
	}
	if got := ResolveChannelAutoGroup(model.AutoGroupTypeNone, false); got != model.AutoGroupTypeNone {
		t.Fatalf("expected explicit none to survive global change, got %d", got)
	}
	// 站点投影渠道在全局开启时仍被强制覆盖。
	if got := ResolveChannelAutoGroup(model.AutoGroupTypeNone, true); got != model.AutoGroupTypeRegex {
		t.Fatalf("expected managed channel to be forced to global mode, got %d", got)
	}
}

// 跟随全局的渠道在全局模式变更后立刻被重新分组，无需逐个去自动分组对话框里选。
func TestRunGroupAutoGroupAppliesGlobalModeToInheritChannels(t *testing.T) {
	setupAutoGroupTestDB(t)
	context := t.Context()

	inherit := autoGroupTestChannel("inherit", "gpt-5.4,claude-4.6", model.AutoGroupTypeInherit)
	if err := ChannelCreate(inherit, context); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	pinned := autoGroupTestChannel("pinned", "gpt-5.4,claude-4.6", model.AutoGroupTypeNone)
	if err := ChannelCreate(pinned, context); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	group := &model.Group{Name: "gpt-5.4", MatchRegex: `^gpt-5\.4$`, Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, context); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}

	// 全局关闭时跟随全局 == 不分组。
	if err := RunGroupAutoGroup(nil, context); err != nil {
		t.Fatalf("RunGroupAutoGroup failed: %v", err)
	}
	if models := autoGroupModels(t, group.ID, inherit.ID); len(models) != 0 {
		t.Fatalf("expected no mappings while global mode is off, got %v", models)
	}

	if err := SettingSetString(model.SettingKeyProjectedChannelAutoGroupEnabled, strconv.Itoa(int(model.AutoGroupTypeRegex))); err != nil {
		t.Fatalf("set global auto group mode: %v", err)
	}
	if err := RunGroupAutoGroup(nil, context); err != nil {
		t.Fatalf("RunGroupAutoGroup failed: %v", err)
	}
	if models := autoGroupModels(t, group.ID, inherit.ID); len(models) != 1 || !containsAllModels(models, "gpt-5.4") {
		t.Fatalf("expected inherit channel to follow global regex mode, got %v", models)
	}
	// 显式设成「不自动分组」的渠道不受全局影响。
	if models := autoGroupModels(t, group.ID, pinned.ID); len(models) != 0 {
		t.Fatalf("expected explicit none channel to stay ungrouped, got %v", models)
	}
}
