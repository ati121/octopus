package op

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/dlclark/regexp2"
)

func ProjectedChannelGlobalAutoGroupMode() model.AutoGroupType {
	value, err := SettingGetString(model.SettingKeyProjectedChannelAutoGroupEnabled)
	if err != nil {
		return model.AutoGroupTypeNone
	}
	mode, ok := model.ParseAutoGroupSettingValue(value)
	if !ok {
		return model.AutoGroupTypeNone
	}
	return mode
}

func ProjectedChannelGlobalAutoGroupEnabled() bool {
	return ProjectedChannelGlobalAutoGroupMode() != model.AutoGroupTypeNone
}

// DefaultChannelAutoGroup 返回新建渠道应落库的自动分组模式。创建渠道时表单没有
// 自动分组开关，若保持「不自动分组」，新渠道要等用户事后到自动分组对话框里逐个
// 补规则，因此未指定时落「跟随全局」——之后全局默认模式变化也会自动生效；
// 显式指定的具体模式原样保留。
func DefaultChannelAutoGroup(requested model.AutoGroupType) model.AutoGroupType {
	if !requested.ValidForChannel() {
		return model.AutoGroupTypeInherit
	}
	if requested == model.AutoGroupTypeNone {
		return model.AutoGroupTypeInherit
	}
	return requested
}

// ResolveChannelAutoGroup 把渠道上存的模式解析成实际生效的模式：
//   - 托管（站点投影）渠道在全局模式开启时始终被全局强制覆盖
//   - 存为「跟随全局」的渠道解析为当前全局默认模式
//   - 其余情况按渠道自身设置
func ResolveChannelAutoGroup(stored model.AutoGroupType, managed bool) model.AutoGroupType {
	global := ProjectedChannelGlobalAutoGroupMode()
	if managed && global != model.AutoGroupTypeNone {
		return global
	}
	if stored == model.AutoGroupTypeInherit {
		return global
	}
	return stored
}

func EffectiveProjectedChannelAutoGroup(channel model.Channel) model.AutoGroupType {
	return ResolveChannelAutoGroup(channel.AutoGroup, true)
}

// ChannelAutoGroupWithMode treats auto grouping as desired state: missing
// mappings are added and stale mappings for this channel are removed.
func ChannelAutoGroupWithMode(channel *model.Channel, autoGroup model.AutoGroupType, ctx context.Context) {
	if channel == nil || autoGroup == model.AutoGroupTypeNone || autoGroup == model.AutoGroupTypeInherit {
		return
	}
	groups, err := GroupList(ctx)
	if err != nil {
		log.Warnf("get group list failed: %v", err)
		return
	}

	channelModelNames := splitChannelModelNames(channel.Model, channel.CustomModel)
	for _, group := range groups {
		desired, ok := matchModelsForAutoGroup(autoGroup, group, channelModelNames, channel.ID)
		if !ok {
			continue
		}
		if err := reconcileGroupItemsForChannel(group, channel.ID, desired, ctx); err != nil {
			log.Warnf("auto group reconcile failed (channel=%d group=%d): %v", channel.ID, group.ID, err)
		}
	}

	if autoGroup == model.AutoGroupTypeExact && AutoGroupCreateMissingEnabled() {
		if err := ensureMissingExactGroups(channel.ID, channelModelNames, groups, ctx); err != nil {
			log.Warnf("auto group create-missing failed (channel=%d): %v", channel.ID, err)
		}
	}
}

func AutoGroupCreateMissingEnabled() bool {
	enabled, err := SettingGetBool(model.SettingKeyAutoGroupCreateMissingEnabled)
	return err == nil && enabled
}

func AutoGroupNormalizeEnabled() bool {
	enabled, err := SettingGetBool(model.SettingKeyAutoGroupNormalizeEnabled)
	return err == nil && enabled
}

func ensureMissingExactGroups(channelID int, channelModelNames []string, existingGroups []model.Group, ctx context.Context) error {
	if channelID <= 0 || len(channelModelNames) == 0 {
		return nil
	}
	normalize := AutoGroupNormalizeEnabled()

	existingByLower := make(map[string]struct{}, len(existingGroups)*2)
	for _, group := range existingGroups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		existingByLower[strings.ToLower(name)] = struct{}{}
		if normalize {
			if normalized := NormalizePublicModelName(name); normalized != "" {
				existingByLower[strings.ToLower(normalized)] = struct{}{}
			}
		}
	}

	type pendingCreate struct {
		groupName  string
		modelNames []string
	}
	pendingByLower := make(map[string]*pendingCreate)

	for _, modelName := range channelModelNames {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		matchedExisting := false
		for _, group := range existingGroups {
			if PublicModelNamesMatch(modelName, group.Name, normalize) {
				matchedExisting = true
				break
			}
		}
		if matchedExisting {
			continue
		}

		groupName := modelName
		if normalize {
			if normalized := NormalizePublicModelName(modelName); normalized != "" {
				groupName = normalized
			}
		}
		lower := strings.ToLower(groupName)
		if _, ok := existingByLower[lower]; ok {
			continue
		}
		if pending, ok := pendingByLower[lower]; ok {
			duplicate := false
			for _, existingModel := range pending.modelNames {
				if existingModel == modelName {
					duplicate = true
					break
				}
			}
			if !duplicate {
				pending.modelNames = append(pending.modelNames, modelName)
			}
			continue
		}
		pendingByLower[lower] = &pendingCreate{groupName: groupName, modelNames: []string{modelName}}
	}

	for _, pending := range pendingByLower {
		group := &model.Group{Name: pending.groupName, Mode: model.GroupModeRoundRobin}
		if err := GroupCreate(group, ctx); err != nil {
			existing, getErr := GroupGetEnabledMap(pending.groupName, ctx)
			if getErr != nil || existing.ID <= 0 {
				return err
			}
			group = &existing
		}
		items := make([]model.GroupIDAndLLMName, 0, len(pending.modelNames))
		for _, modelName := range pending.modelNames {
			items = append(items, model.GroupIDAndLLMName{ChannelID: channelID, ModelName: modelName})
		}
		if err := GroupItemBatchAdd(group.ID, items, ctx); err != nil {
			return err
		}
		existingByLower[strings.ToLower(pending.groupName)] = struct{}{}
	}
	return nil
}

func matchModelsForAutoGroup(autoGroup model.AutoGroupType, group model.Group, channelModelNames []string, channelID int) ([]string, bool) {
	matched := make([]string, 0)
	switch autoGroup {
	case model.AutoGroupTypeExact:
		normalize := AutoGroupNormalizeEnabled()
		for _, modelName := range channelModelNames {
			if PublicModelNamesMatch(modelName, group.Name, normalize) {
				matched = append(matched, modelName)
			}
		}
		return matched, true
	case model.AutoGroupTypeFuzzy:
		groupName := strings.ToLower(strings.TrimSpace(group.Name))
		if groupName == "" {
			return nil, true
		}
		for _, modelName := range channelModelNames {
			if strings.Contains(strings.ToLower(modelName), groupName) {
				matched = append(matched, modelName)
			}
		}
		return matched, true
	case model.AutoGroupTypeRegex:
		if group.MatchRegex == "" {
			for _, modelName := range channelModelNames {
				if strings.EqualFold(modelName, group.Name) {
					matched = append(matched, modelName)
				}
			}
			return matched, true
		}
		re, err := regexp2.Compile(group.MatchRegex, regexp2.ECMAScript)
		if err != nil {
			log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channelID, group.ID, group.MatchRegex, err)
			return nil, false
		}
		re.MatchTimeout = 200 * time.Millisecond
		for _, modelName := range channelModelNames {
			matches, err := re.MatchString(modelName)
			if err != nil {
				log.Warnf("match regex failed (channel=%d group=%d regex=%q model=%q): %v", channelID, group.ID, group.MatchRegex, modelName, err)
				continue
			}
			if matches {
				matched = append(matched, modelName)
			}
		}
		return matched, true
	default:
		return nil, false
	}
}

func reconcileGroupItemsForChannel(group model.Group, channelID int, desired []string, ctx context.Context) error {
	desiredSet := make(map[string]struct{}, len(desired))
	for _, name := range desired {
		desiredSet[name] = struct{}{}
	}

	existing := make([]string, 0)
	for _, item := range group.Items {
		if item.ChannelID == channelID {
			existing = append(existing, item.ModelName)
		}
	}

	toAdd := make([]model.GroupIDAndLLMName, 0)
	for _, name := range desired {
		found := false
		for _, current := range existing {
			if current == name {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, model.GroupIDAndLLMName{ChannelID: channelID, ModelName: name})
		}
	}

	toRemove := make([]string, 0)
	for _, current := range existing {
		if _, ok := desiredSet[current]; !ok {
			toRemove = append(toRemove, current)
		}
	}
	if err := GroupItemBatchDelInGroup(group.ID, channelID, toRemove, ctx); err != nil {
		return err
	}
	if len(toAdd) > 0 {
		return GroupItemBatchAdd(group.ID, toAdd, ctx)
	}
	return nil
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	ChannelAutoGroupWithMode(channel, ResolveChannelAutoGroup(channel.AutoGroup, false), ctx)
}

func splitChannelModelNames(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

func ValidateJSONOverrideObject(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return fmt.Errorf("param_override must be a JSON object")
	}
	return nil
}
