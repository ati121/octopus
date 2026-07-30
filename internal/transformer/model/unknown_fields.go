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
	for key, value := range raw {
		if _, ok := known[key]; ok {
			continue
		}
		if unknown == nil {
			unknown = make(map[string]json.RawMessage, 4)
		}
		unknown[key] = value
	}
	if unknown != nil {
		req.UnknownFields = unknown
	}
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
