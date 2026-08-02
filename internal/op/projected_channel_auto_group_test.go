package op

import (
	"path/filepath"
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

	// 全局未开启时保持原样：新渠道仍然是「不自动分组」。
	if got := DefaultChannelAutoGroup(model.AutoGroupTypeNone); got != model.AutoGroupTypeNone {
		t.Fatalf("expected none without global mode, got %d", got)
	}

	if err := SettingSetString(model.SettingKeyProjectedChannelAutoGroupEnabled, strconv.Itoa(int(model.AutoGroupTypeRegex))); err != nil {
		t.Fatalf("set global auto group mode: %v", err)
	}
	if got := DefaultChannelAutoGroup(model.AutoGroupTypeNone); got != model.AutoGroupTypeRegex {
		t.Fatalf("expected new channel to inherit global regex mode, got %d", got)
	}
	// 显式指定的模式不被全局覆盖。
	if got := DefaultChannelAutoGroup(model.AutoGroupTypeFuzzy); got != model.AutoGroupTypeFuzzy {
		t.Fatalf("expected explicit fuzzy mode to survive, got %d", got)
	}
}
