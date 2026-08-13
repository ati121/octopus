package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 上游请求头 / 参数覆盖的全局与模型规则层。
//
// 分层与优先级（后者覆盖前者）：请求体 < 全局 < 模型规则 < 渠道覆盖。
// 渠道层已有实现见 Channel.CustomHeader 与 Channel.ParamOverride，本文件只描述
// 全局层与模型规则层的存储结构；四层的实际落地在 relay 的 copyHeaders /
// applyParamOverride 两处。
//
// 全局层直接存原始 JSON 文本（请求头为数组、参数覆盖为对象），模型规则层存规则数组。

// UpstreamHeaderRule 是按模型匹配的请求头规则。
// Models 为逗号分隔的通配符模式，匹配「解析后的上游模型名」，大小写不敏感。
type UpstreamHeaderRule struct {
	Models  string         `json:"models"`
	Headers []CustomHeader `json:"headers"`
}

// UpstreamParamRule 是按模型匹配的请求体参数覆盖规则。
// ParamOverride 用 json.RawMessage 承载，避免入库时出现二次转义。
type UpstreamParamRule struct {
	Models        string          `json:"models"`
	ParamOverride json.RawMessage `json:"param_override"`
}

// ParseUpstreamHeaders 解析全局请求头设置值。空值视为无配置。
func ParseUpstreamHeaders(value string) ([]CustomHeader, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	var headers []CustomHeader
	if err := json.Unmarshal([]byte(trimmed), &headers); err != nil {
		return nil, fmt.Errorf("setting value must be a JSON array of headers: %w", err)
	}
	return headers, nil
}

// ParseUpstreamHeaderRules 解析模型请求头规则设置值。空值视为无规则。
func ParseUpstreamHeaderRules(value string) ([]UpstreamHeaderRule, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	var rules []UpstreamHeaderRule
	if err := json.Unmarshal([]byte(trimmed), &rules); err != nil {
		return nil, fmt.Errorf("setting value must be a JSON array of header rules: %w", err)
	}
	return rules, nil
}

// ParseUpstreamParamRules 解析模型参数覆盖规则设置值。空值视为无规则。
func ParseUpstreamParamRules(value string) ([]UpstreamParamRule, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	var rules []UpstreamParamRule
	if err := json.Unmarshal([]byte(trimmed), &rules); err != nil {
		return nil, fmt.Errorf("setting value must be a JSON array of param rules: %w", err)
	}
	return rules, nil
}

// validateUpstreamHeaders 校验请求头数组：至少要有非空的 header 名。
func validateUpstreamHeaders(value string) error {
	headers, err := ParseUpstreamHeaders(value)
	if err != nil {
		return err
	}
	return validateHeaderEntries(headers)
}

// validateUpstreamHeaderRules 校验模型请求头规则数组。
func validateUpstreamHeaderRules(value string) error {
	rules, err := ParseUpstreamHeaderRules(value)
	if err != nil {
		return err
	}
	for i, rule := range rules {
		if strings.TrimSpace(rule.Models) == "" {
			return fmt.Errorf("rule %d: models must not be empty", i+1)
		}
		if err := validateHeaderEntries(rule.Headers); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
	}
	return nil
}

// validateUpstreamParamOverride 校验全局参数覆盖：必须为空或 JSON 对象，且不得覆盖 model。
func validateUpstreamParamOverride(value string) error {
	return ValidateUpstreamParamOverrideJSON([]byte(value))
}

// validateUpstreamParamRules 校验模型参数覆盖规则数组。
func validateUpstreamParamRules(value string) error {
	rules, err := ParseUpstreamParamRules(value)
	if err != nil {
		return err
	}
	for i, rule := range rules {
		if strings.TrimSpace(rule.Models) == "" {
			return fmt.Errorf("rule %d: models must not be empty", i+1)
		}
		if err := ValidateUpstreamParamOverrideJSON(rule.ParamOverride); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
	}
	return nil
}

// ValidateUpstreamParamOverrideJSON 校验一段参数覆盖 JSON：
// 空值放行；非空时必须是 JSON 对象；禁止包含 model 键。
//
// 禁止 model 的原因：覆盖在最终请求体字节层生效，若能改写 model 会静默击穿
// 分组的模型映射结果，并让熔断 / 负载均衡按模型统计的口径错位。
// 渠道级 ParamOverride 为兼容既有配置不做此限制。
func ValidateUpstreamParamOverrideJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return fmt.Errorf("param override must be a JSON object: %w", err)
	}
	for key := range decoded {
		if strings.EqualFold(strings.TrimSpace(key), "model") {
			return fmt.Errorf("param override must not contain the model field")
		}
	}
	return nil
}

// validateHeaderEntries 校验请求头条目：不允许出现空的 header 名。
func validateHeaderEntries(headers []CustomHeader) error {
	for i, header := range headers {
		if strings.TrimSpace(header.HeaderKey) == "" {
			return fmt.Errorf("header %d: header key must not be empty", i+1)
		}
	}
	return nil
}
