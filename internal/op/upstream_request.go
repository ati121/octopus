package op

import (
	"strings"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
)

// 全局与模型规则层的上游请求头 / 参数覆盖读取。
//
// 这些值在中继热路径上每请求都要读，不能每次都做 JSON 解析：
// 用 parsedSetting 做「原文没变就复用上次解析结果」的记忆化。
// 原文取自 settingCache（内存，不查库），因此设置更新后下一次读取自然生效，
// 无需额外的失效通知机制。

// parsedSetting 缓存一个 setting 的解析结果，raw 变化时重新解析。
type parsedSetting[T any] struct {
	state atomic.Pointer[parsedSettingState[T]]
}

type parsedSettingState[T any] struct {
	raw   string
	value T
}

// get 返回 key 当前值的解析结果。解析失败时返回零值，
// 并把失败的原文一并缓存，避免每请求都重复报错日志。
func (p *parsedSetting[T]) get(key model.SettingKey, parse func(string) (T, error)) T {
	raw, err := SettingGetString(key)
	if err != nil {
		var zero T
		return zero
	}
	if state := p.state.Load(); state != nil && state.raw == raw {
		return state.value
	}
	value, parseErr := parse(raw)
	if parseErr != nil {
		log.Warnf("failed to parse setting %s, ignoring it: %v", key, parseErr)
		var zero T
		value = zero
	}
	p.state.Store(&parsedSettingState[T]{raw: raw, value: value})
	return value
}

var (
	upstreamGlobalHeadersCache   parsedSetting[[]model.CustomHeader]
	upstreamModelHeaderRuleCache parsedSetting[[]model.UpstreamHeaderRule]
	upstreamModelParamRuleCache  parsedSetting[[]model.UpstreamParamRule]
)

// UpstreamGlobalHeaders 返回全局上游请求头。无配置时返回 nil。
func UpstreamGlobalHeaders() []model.CustomHeader {
	return upstreamGlobalHeadersCache.get(model.SettingKeyUpstreamGlobalHeaders, model.ParseUpstreamHeaders)
}

// UpstreamModelHeadersFor 返回命中 modelName 的模型规则请求头，按规则声明顺序拼接。
// 多条规则命中同一模型时后者覆盖前者（调用方按序 Set 即可）。
func UpstreamModelHeadersFor(modelName string) []model.CustomHeader {
	rules := upstreamModelHeaderRuleCache.get(model.SettingKeyUpstreamModelHeaderRules, model.ParseUpstreamHeaderRules)
	if len(rules) == 0 {
		return nil
	}
	var headers []model.CustomHeader
	for _, rule := range rules {
		if !xstrings.MatchAnyWildcard(rule.Models, modelName) {
			continue
		}
		headers = append(headers, rule.Headers...)
	}
	return headers
}

// UpstreamParamOverrideChain 返回作用于 modelName 的参数覆盖层，顺序为「全局 → 命中的模型规则」。
// 调用方需在末尾追加渠道级覆盖，保证渠道优先级最高。全部为空时返回 nil。
func UpstreamParamOverrideChain(modelName string) [][]byte {
	var chain [][]byte
	if global, err := SettingGetString(model.SettingKeyUpstreamGlobalParamOverride); err == nil && !isBlankParamOverride(global) {
		chain = append(chain, []byte(global))
	}

	rules := upstreamModelParamRuleCache.get(model.SettingKeyUpstreamModelParamRules, model.ParseUpstreamParamRules)
	for _, rule := range rules {
		if isBlankParamOverride(string(rule.ParamOverride)) {
			continue
		}
		if !xstrings.MatchAnyWildcard(rule.Models, modelName) {
			continue
		}
		chain = append(chain, rule.ParamOverride)
	}
	return chain
}

// isBlankParamOverride 判断一层覆盖是否等价于「无覆盖」。
// 默认值 "{}" 与 null 在这里被剔除，让未配置时链为空、完全不触碰请求体字节。
// 其余等价写法（如 "{ }"）由 helper 层的合并逻辑兜底跳过。
func isBlankParamOverride(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "{}", "null":
		return true
	}
	return false
}
