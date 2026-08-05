package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
)

// 字段保全（F-1）：跨版本前向兼容。
//
// 背景：inbound 通过 json.Unmarshal 把客户端请求体反序列化到 InternalLLMRequest，
// struct 未显式声明的顶层字段会被静默丢弃。对于「同格式准直通」场景
// （目前仅 OpenAI Chat → OpenAI Chat），客户端可能使用了 octopus 尚未建模的新参数，
// 直接丢弃会导致上游收不到该参数。
//
// 方案：inbound 侧用 CaptureUnknownRequestFields 把未建模的顶层字段捕获到
// InternalLLMRequest.UnknownFields；outbound 侧在「相同 wire 格式」时用
// MergeUnknownFields 把它们合并回最终请求体。跨格式路径不合并，从而保持
// Chat outbound 白名单对字段泄漏的防护。
//
// 例外：驼峰写法的已建模字段（promptCacheKey 之于 prompt_cache_key）不算未知，
// 会在捕获阶段按正名收编，见 CaptureUnknownRequestFields。

// knownRequestJSONKeys 缓存 InternalLLMRequest 上所有带 json tag（且非 "-"）的顶层
// 字段名集合，用于在 unmarshal 后判定哪些客户端顶层字段是「未知」的。
var (
	knownRequestJSONKeysOnce sync.Once
	knownRequestJSONKeys     map[string]struct{}
)

func requestKnownJSONKeys() map[string]struct{} {
	knownRequestJSONKeysOnce.Do(func() {
		knownRequestJSONKeys = collectJSONKeys(reflect.TypeOf(InternalLLMRequest{}))
	})
	return knownRequestJSONKeys
}

// collectJSONKeys 提取 struct 类型上所有 json tag 名（跳过 tag 为 "-" 的内部字段）。
// 匿名内嵌字段递归展开，以覆盖未来可能引入的组合结构。
func collectJSONKeys(t reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{})
	if t == nil || t.Kind() != reflect.Struct {
		return keys
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			// 无 json tag 的匿名内嵌 struct：递归展开其字段名。
			if field.Anonymous {
				fieldType := field.Type
				if fieldType.Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}
				for k := range collectJSONKeys(fieldType) {
					keys[k] = struct{}{}
				}
			}
			continue
		}
		keys[name] = struct{}{}
	}
	return keys
}

// CaptureUnknownRequestFields 解析原始请求体，收集 InternalLLMRequest 未建模的顶层字段，
// 存入 req.UnknownFields。仅供选择「同格式保全」的 inbound 调用（当前为 OpenAI Chat）。
//
// 驼峰别名先被收编：部分客户端（如把 TS 侧选项名直接塞进请求体的 SDK）会发出
// promptCacheKey 这类驼峰键，snake 化后若命中已建模字段，就按正名解析进 req，
// 不再进 UnknownFields。否则它会被原样转发给上游，而严格的上游会以
// UNKNOWN_FIELD 拒绝整轮请求（实测 tokenrhythm 对 promptCacheKey 返回 400）。
//
// body 非合法 JSON 对象时静默跳过（此时上层 unmarshal 也会报错，无需重复处理）。
func CaptureUnknownRequestFields(req *InternalLLMRequest, body []byte) {
	if req == nil || len(body) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return
	}
	known := requestKnownJSONKeys()
	var unknown map[string]json.RawMessage
	var aliased map[string]json.RawMessage
	for key, value := range raw {
		if _, ok := known[key]; ok {
			continue
		}
		if snake, ok := knownSnakeAlias(known, raw, key); ok {
			if aliased == nil {
				aliased = make(map[string]json.RawMessage, 2)
			}
			aliased[snake] = value
			continue
		}
		if unknown == nil {
			unknown = make(map[string]json.RawMessage, 4)
		}
		unknown[key] = value
	}
	applyAliasedFields(req, aliased)
	if unknown != nil {
		req.UnknownFields = unknown
	}
}

// knownSnakeAlias 判定 key 是否为某个已建模字段的驼峰别名，是则返回其 snake_case 正名。
// 客户端同时发了正名时不收编（正名已由上层 unmarshal 解析），别名直接丢弃，
// 避免重复字段仍然泄漏到上游。
func knownSnakeAlias(known map[string]struct{}, raw map[string]json.RawMessage, key string) (string, bool) {
	snake := toSnakeCase(key)
	if snake == key {
		return "", false
	}
	if _, ok := known[snake]; !ok {
		return "", false
	}
	if _, dup := raw[snake]; dup {
		return "", true
	}
	return snake, true
}

// applyAliasedFields 把收编的别名按正名解析进 req。
// 逐个字段单独 unmarshal，避免某个值类型不匹配时连累后面的字段；
// 解析失败的字段保持零值（等价于客户端没发），不回退成原样转发。
func applyAliasedFields(req *InternalLLMRequest, aliased map[string]json.RawMessage) {
	for name, value := range aliased {
		patch, err := json.Marshal(map[string]json.RawMessage{name: value})
		if err != nil {
			continue
		}
		// 先在弃用副本上试解析。类型不匹配时 encoding/json 会先分配目标
		// （指针字段变成指向零值的非 nil 指针）再报错，直接写进 req 会让上游
		// 收到 "prompt_cache_key":"" 这类空值，比丢弃更糟。
		var probe InternalLLMRequest
		if err := json.Unmarshal(patch, &probe); err != nil {
			continue
		}
		_ = json.Unmarshal(patch, req)
	}
}

// toSnakeCase 把驼峰名转成 snake_case：promptCacheKey → prompt_cache_key。
// 非字母字符原样保留，已是 snake_case 或全小写的键会原样返回。
func toSnakeCase(name string) string {
	if name == "" {
		return name
	}
	var b strings.Builder
	b.Grow(len(name) + 4)
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// MergeUnknownFields 把 req.UnknownFields 合并进已序列化的请求体 body。
// 仅在 outbound 与 inbound 为相同 wire 格式时调用（同格式保全）。
//
// 合并策略：只填充 body 中尚不存在的顶层 key，绝不覆盖 outbound 已显式写入的字段，
// 避免破坏 outbound 的规范化结果。body 非 JSON 对象时原样返回。
func MergeUnknownFields(body []byte, unknown map[string]json.RawMessage) []byte {
	if len(unknown) == 0 || len(body) == 0 {
		return body
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	changed := false
	for key, value := range unknown {
		if _, exists := obj[key]; exists {
			continue
		}
		obj[key] = value
		changed = true
	}
	if !changed {
		return body
	}
	merged, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return merged
}
