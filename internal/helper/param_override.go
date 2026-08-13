package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/utils/iolimit"
)

// ApplyParamOverride 把单层 JSON 对象覆盖深合并进出站 JSON 请求体。
// 空覆盖、nil 请求体与非对象请求体均不做处理。
func ApplyParamOverride(request *http.Request, paramOverride *string) error {
	if paramOverride == nil {
		return nil
	}
	return ApplyParamOverrides(request, []byte(*paramOverride))
}

// ApplyParamOverrides 按顺序把多层 JSON 对象覆盖深合并进出站 JSON 请求体，后者覆盖前者。
// 调用方按「全局 < 模型规则 < 渠道覆盖」的顺序传入。
//
// 合并语义为深合并：两侧同为 JSON 对象时递归合并；其余情况（数组、标量、两侧类型不一致）
// 整体替换——tools / messages 这类数组语义上只能整体替换。
//
// 所有层都为空（空串 / 空白 / 空对象）时完全不触碰 request.Body。
// 这一点对透传路径很关键：透传的请求体是逐字节保真的客户端 JSON，
// Anthropic prompt-cache 依赖其字节布局，而 map 往返会重排键序，不能无谓改写。
func ApplyParamOverrides(request *http.Request, overrides ...[]byte) error {
	if request == nil || request.Body == nil {
		return nil
	}
	merged := mergeOverrideLayers(overrides)
	if len(merged) == 0 {
		return nil
	}

	body, err := iolimit.ReadAll(request.Body, iolimit.RequestBodyMaxBytes())
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	setBody := func(payload []byte) {
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.ContentLength = int64(len(payload))
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		}
	}

	// 非 JSON 对象请求体（multipart、JSON 数组、null 等）原样恢复，不视为错误。
	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil || bodyMap == nil {
		setBody(body)
		return nil
	}

	deepMergeInto(bodyMap, merged)

	modifiedBody, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal request body with param override: %w", err)
	}
	setBody(modifiedBody)
	return nil
}

// mergeOverrideLayers 依次解析各层覆盖并深合并成一个 map，后者覆盖前者。
// 空白层、非 JSON 对象层与空对象层被静默跳过，与既有的宽容行为一致。
// 每次调用都重新解析，因此返回的 map 不会与其它请求共享嵌套引用。
func mergeOverrideLayers(overrides [][]byte) map[string]any {
	var merged map[string]any
	for _, override := range overrides {
		if strings.TrimSpace(string(override)) == "" {
			continue
		}
		var layer map[string]any
		if err := json.Unmarshal(override, &layer); err != nil || len(layer) == 0 {
			continue
		}
		if merged == nil {
			merged = make(map[string]any, len(layer))
		}
		deepMergeInto(merged, layer)
	}
	return merged
}

// deepMergeInto 把 src 深合并进 dst：两侧同为对象时递归合并，其余情况整体替换。
func deepMergeInto(dst, src map[string]any) {
	for key, srcValue := range src {
		srcMap, srcIsMap := srcValue.(map[string]any)
		if !srcIsMap {
			dst[key] = srcValue
			continue
		}
		dstMap, dstIsMap := dst[key].(map[string]any)
		if !dstIsMap {
			dst[key] = srcValue
			continue
		}
		deepMergeInto(dstMap, srcMap)
	}
}
