package model

import (
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// siteModelFilterMatchTimeout 与 helper.FetchModels 的渠道同步过滤正则保持一致，
// 防止灾难性回溯把保存请求或后台同步卡死。
const siteModelFilterMatchTimeout = 200 * time.Millisecond

// CompileSiteModelFilterRegex 编译分组级模型正则筛选。
// 返回 (nil, nil) 表示未配置正则——留空即不筛选，该分组模型全部启用。
// 使用 ECMAScript 模式与渠道的 match_regex 语义保持一致（支持 (?i)(?s)(?m) 内联 flag）。
func CompileSiteModelFilterRegex(pattern string) (*regexp2.Regexp, error) {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return nil, nil
	}
	re, err := regexp2.Compile(trimmed, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = siteModelFilterMatchTimeout
	return re, nil
}

// ValidateSiteModelFilterRegex 在写入口校验正则。写错的正则会让之后每一轮同步都按
// 脏数据处理，因此必须在保存前挡掉。
func ValidateSiteModelFilterRegex(pattern string) error {
	_, err := CompileSiteModelFilterRegex(pattern)
	return err
}

// SiteModelFilterAllows 判断模型名是否被正则选中。re 为 nil（未配置正则）时全部放行。
// 匹配本身出错（例如回溯超时）时同样放行：宁可少停用，也不要把整组模型误杀。
func SiteModelFilterAllows(re *regexp2.Regexp, modelName string) bool {
	if re == nil {
		return true
	}
	matched, err := re.MatchString(strings.TrimSpace(modelName))
	if err != nil {
		return true
	}
	return matched
}

// SiteModelOverrideKey 生成用户表态记录的查找键。
func SiteModelOverrideKey(accountID int, groupKey string, modelName string) string {
	return strconv.Itoa(accountID) + "\x00" + NormalizeSiteGroupKey(groupKey) + "\x00" + strings.TrimSpace(modelName)
}

// SiteModelDisabledOverrides 是按 SiteModelOverrideKey 索引的用户表态集合，
// value 为用户选择的停用状态。
type SiteModelDisabledOverrides map[string]bool

// NewSiteModelDisabledOverrides 把 site_model_state_overrides 的行索引成查找表。
func NewSiteModelDisabledOverrides(rows []SiteModelStateOverride) SiteModelDisabledOverrides {
	if len(rows) == 0 {
		return nil
	}
	index := make(SiteModelDisabledOverrides, len(rows))
	for _, row := range rows {
		index[SiteModelOverrideKey(row.SiteAccountID, row.GroupKey, row.ModelName)] = row.Disabled
	}
	return index
}

// Lookup 返回该模型是否有用户表态，以及表态的内容。
func (o SiteModelDisabledOverrides) Lookup(accountID int, groupKey string, modelName string) (bool, bool) {
	if len(o) == 0 {
		return false, false
	}
	disabled, ok := o[SiteModelOverrideKey(accountID, groupKey, modelName)]
	return disabled, ok
}

// ResolveSiteModelDisabled 是站点模型启用状态的唯一判定规则：
//
//	用户表态存在 -> 直接用用户的选择（正则只是批量默认值，手动是逐个例外）
//	否则分组配了正则 -> 命中即启用，未命中即停用
//	否则 -> 启用
//
// re 为 nil（未配置或非法正则）时 SiteModelFilterAllows 全部放行，因此结果是启用。
// 判定必须只依赖这三项输入：site_models.disabled 是由本函数算出的派生列，不能反过来
// 参与判定，否则同步删行重建时状态就会漂移。
func ResolveSiteModelDisabled(override *bool, re *regexp2.Regexp, modelName string) bool {
	if override != nil {
		return *override
	}
	return !SiteModelFilterAllows(re, modelName)
}

// ApplySiteModelDisabledState 按 ResolveSiteModelDisabled 重算整批模型的 Disabled。
//
// filters 的 key 是分组 key，value 是该分组配置的正则原文；正则留空或非法的分组视为
// 不筛选。overrides 为用户表态索引，优先级最高。返回实际被改动的条目数。
func ApplySiteModelDisabledState(models []SiteModel, filters map[string]string, overrides SiteModelDisabledOverrides) int {
	if len(models) == 0 {
		return 0
	}

	compiled := make(map[string]*regexp2.Regexp, len(filters))
	for groupKey, pattern := range filters {
		re, err := CompileSiteModelFilterRegex(pattern)
		if err != nil || re == nil {
			continue
		}
		compiled[NormalizeSiteGroupKey(groupKey)] = re
	}

	changed := 0
	for i := range models {
		groupKey := NormalizeSiteGroupKey(models[i].GroupKey)
		var override *bool
		if disabled, ok := overrides.Lookup(models[i].SiteAccountID, groupKey, models[i].ModelName); ok {
			override = &disabled
		}
		nextDisabled := ResolveSiteModelDisabled(override, compiled[groupKey], models[i].ModelName)
		if models[i].Disabled != nextDisabled {
			models[i].Disabled = nextDisabled
			changed++
		}
	}
	return changed
}
