package op

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

type EnsurePublicGroupsResult struct {
	AccountID         int      `json:"account_id"`
	SiteID            int      `json:"site_id"`
	ChannelsProcessed int      `json:"channels_processed"`
	GroupsCreated     int      `json:"groups_created"`
	ItemsAdded        int      `json:"items_added"`
	CreatedGroupNames []string `json:"created_group_names,omitempty"`
	Normalize         bool     `json:"normalize"`
	Message           string   `json:"message"`
}

// EnsurePublicGroupsForSiteAccount performs a one-shot exact reconcile for
// all projected channels of an account and always creates missing groups.
func EnsurePublicGroupsForSiteAccount(siteID, accountID int, ctx context.Context) (*EnsurePublicGroupsResult, error) {
	if siteID <= 0 || accountID <= 0 {
		return nil, newSiteChannelAccountNotFoundError()
	}

	site, err := SiteGet(siteID, ctx)
	if err != nil {
		return nil, err
	}
	var account *model.SiteAccount
	for index := range site.Accounts {
		if site.Accounts[index].ID == accountID {
			account = &site.Accounts[index]
			break
		}
	}
	if account == nil {
		return nil, newSiteChannelAccountNotFoundError()
	}

	channelIDs := make([]int, 0, len(account.ChannelBindings))
	channelSet := make(map[int]struct{}, len(account.ChannelBindings))
	for _, binding := range account.ChannelBindings {
		if binding.ChannelID <= 0 {
			continue
		}
		if _, exists := channelSet[binding.ChannelID]; exists {
			continue
		}
		channelSet[binding.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, binding.ChannelID)
	}

	result := &EnsurePublicGroupsResult{
		AccountID: accountID,
		SiteID:    siteID,
		Normalize: AutoGroupNormalizeEnabled(),
	}
	beforeGroups, err := GroupList(ctx)
	if err != nil {
		return nil, err
	}
	beforeNames := make(map[string]struct{}, len(beforeGroups))
	for _, group := range beforeGroups {
		beforeNames[strings.ToLower(strings.TrimSpace(group.Name))] = struct{}{}
	}
	itemsBefore := countGroupItemsForChannels(beforeGroups, channelSet)

	for _, channelID := range channelIDs {
		channel, err := ChannelGet(channelID, ctx)
		if err != nil || channel == nil || !channel.Enabled {
			continue
		}
		ensurePublicGroupsForChannel(channel, ctx)
		result.ChannelsProcessed++
	}

	afterGroups, err := GroupList(ctx)
	if err != nil {
		return nil, err
	}
	for _, group := range afterGroups {
		key := strings.ToLower(strings.TrimSpace(group.Name))
		if _, existed := beforeNames[key]; !existed {
			result.CreatedGroupNames = append(result.CreatedGroupNames, group.Name)
		}
	}
	result.GroupsCreated = len(result.CreatedGroupNames)
	itemsAfter := countGroupItemsForChannels(afterGroups, channelSet)
	if itemsAfter > itemsBefore {
		result.ItemsAdded = itemsAfter - itemsBefore
	}

	parts := []string{
		fmt.Sprintf("处理 %d 个投影渠道", result.ChannelsProcessed),
		fmt.Sprintf("新建 %d 个对外分组", result.GroupsCreated),
		fmt.Sprintf("新增 %d 条挂载", result.ItemsAdded),
	}
	if result.Normalize {
		parts = append(parts, "已启用模型名归一化")
	}
	result.Message = strings.Join(parts, "，")
	return result, nil
}

func countGroupItemsForChannels(groups []model.Group, channelIDs map[int]struct{}) int {
	total := 0
	for _, group := range groups {
		for _, item := range group.Items {
			if _, ok := channelIDs[item.ChannelID]; ok {
				total++
			}
		}
	}
	return total
}

func ensurePublicGroupsForChannel(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	groups, err := GroupList(ctx)
	if err != nil {
		return
	}
	modelNames := splitChannelModelNames(channel.Model, channel.CustomModel)
	for _, group := range groups {
		desired, ok := matchModelsForAutoGroup(model.AutoGroupTypeExact, group, modelNames, channel.ID)
		if ok {
			_ = reconcileGroupItemsForChannel(group, channel.ID, desired, ctx)
		}
	}
	_ = ensureMissingExactGroups(channel.ID, modelNames, groups, ctx)
}
