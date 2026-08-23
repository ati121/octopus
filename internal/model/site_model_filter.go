package model

import (
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

// ApplySiteModelFilters 按分组正则重算 models 的 Disabled 状态：命中正则的启用、
// 未命中的停用。
//
// filters 的 key 是分组 key，value 是该分组配置的正则原文。**只有正则非空的分组会被
// 处理**——正则留空表示该分组不做筛选，此时保持模型现有的启用状态不动，以免每轮同步
// 把用户手动停用的模型重新翻上来。“清空正则即全部启用”只在保存正则那一刻生效
// （见 op.UpdateSiteGroupModelFilter）。
//
// 正则本身非法的分组同样整组跳过并保持既有状态，避免脏数据把模型全部误停用。
// 返回实际被改动的条目数。
func ApplySiteModelFilters(models []SiteModel, filters map[string]string) int {
	if len(models) == 0 || len(filters) == 0 {
		return 0
	}

	compiled := make(map[string]*regexp2.Regexp, len(filters))
	for groupKey, pattern := range filters {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		re, err := CompileSiteModelFilterRegex(pattern)
		if err != nil || re == nil {
			continue
		}
		compiled[NormalizeSiteGroupKey(groupKey)] = re
	}
	if len(compiled) == 0 {
		return 0
	}

	changed := 0
	for i := range models {
		re, ok := compiled[NormalizeSiteGroupKey(models[i].GroupKey)]
		if !ok {
			continue
		}
		nextDisabled := !SiteModelFilterAllows(re, models[i].ModelName)
		if models[i].Disabled != nextDisabled {
			models[i].Disabled = nextDisabled
			changed++
		}
	}
	return changed
}
