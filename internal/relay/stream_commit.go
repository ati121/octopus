package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/tmaxmax/go-sse"
)

type deferredStreamWriter struct {
	writer          StreamWriter
	pending         bytes.Buffer
	committed       bool
	rejectedPayload []byte
}

func newDeferredStreamWriter(writer StreamWriter) *deferredStreamWriter {
	return &deferredStreamWriter{writer: writer}
}

func (w *deferredStreamWriter) Write(data []byte) (int, error) {
	if w.committed {
		return w.writer.Write(data)
	}
	if isSSEHeartbeat(data) {
		n, err := w.writer.Write(data)
		if err == nil {
			w.writer.Flush()
		}
		return n, err
	}

	_, _ = w.pending.Write(data)
	commit, err := inspectDeferredStream(w.pending.Bytes())
	if err != nil {
		w.rejectedPayload = append(w.rejectedPayload[:0], w.pending.Bytes()...)
		return 0, err
	}
	if !commit {
		return len(data), nil
	}

	payload := append([]byte(nil), w.pending.Bytes()...)
	w.pending.Reset()
	if _, err := w.writer.Write(payload); err != nil {
		return 0, err
	}
	w.committed = true
	return len(data), nil
}

func (w *deferredStreamWriter) Flush() {
	if w.committed {
		w.writer.Flush()
	}
}

func (w *deferredStreamWriter) Written() bool {
	return w.committed
}

func (w *deferredStreamWriter) Header() http.Header {
	return w.writer.Header()
}

func (w *deferredStreamWriter) WriteHeader(code int) {
	w.writer.WriteHeader(code)
}

func (w *deferredStreamWriter) RejectedPayload() []byte {
	return append([]byte(nil), w.rejectedPayload...)
}

func isSSEHeartbeat(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && bytes.HasPrefix(trimmed, []byte(":")) && !bytes.Contains(trimmed, []byte("data:"))
}

func inspectDeferredStream(data []byte) (bool, error) {
	readConfig := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for event, readErr := range sse.Read(bytes.NewReader(data), readConfig) {
		if readErr != nil {
			break
		}
		payload := strings.TrimSpace(event.Data)
		if payload == "[DONE]" {
			return true, nil
		}

		var envelope map[string]any
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			if isFailureStreamEvent(event.Type) {
				return false, newStreamResponseError(event.Type, nil)
			}
			continue
		}

		eventType := strings.TrimSpace(event.Type)
		if eventType == "" {
			eventType = stringField(envelope, "type")
		}
		if err := streamEnvelopeError(eventType, envelope); err != nil {
			return false, err
		}
		if streamEnvelopeCommits(eventType, envelope) {
			return true, nil
		}
	}
	return false, nil
}

func streamEnvelopeError(eventType string, envelope map[string]any) error {
	if isFailureStreamEvent(eventType) {
		return newStreamResponseError(eventType, envelope)
	}
	if nestedMap(envelope, "error") != nil {
		return newStreamResponseError(eventType, envelope)
	}
	response := nestedMap(envelope, "response")
	if response != nil && nestedMap(response, "error") != nil {
		return newStreamResponseError(eventType, envelope)
	}
	return nil
}

func streamEnvelopeCommits(eventType string, envelope map[string]any) bool {
	switch eventType {
	case "message_stop", "response.completed", "response.incomplete":
		return true
	case "message_delta":
		return strings.TrimSpace(stringField(nestedMap(envelope, "delta"), "stop_reason")) != ""
	case "content_block_start":
		block := nestedMap(envelope, "content_block")
		blockType := stringField(block, "type")
		if blockType == "tool_use" || blockType == "server_tool_use" {
			return true
		}
		return hasMeaningfulValue(block, "text", "thinking", "data", "input")
	case "content_block_delta":
		delta := nestedMap(envelope, "delta")
		return hasMeaningfulValue(delta, "text", "thinking", "partial_json")
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.refusal.delta", "response.function_call_arguments.delta":
		return strings.TrimSpace(stringField(envelope, "delta")) != ""
	case "response.output_item.added":
		item := nestedMap(envelope, "item")
		return stringField(item, "type") == "function_call"
	}

	choices, _ := envelope["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		if strings.TrimSpace(stringField(choice, "finish_reason")) != "" {
			return true
		}
		for _, key := range []string{"delta", "message"} {
			message := nestedMap(choice, key)
			if message == nil {
				continue
			}
			if hasMeaningfulValue(message, "content", "refusal", "reasoning", "reasoning_content") {
				return true
			}
			if toolCalls, ok := message["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				return true
			}
		}
	}
	return false
}

func isFailureStreamEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "error", "response.failed":
		return true
	default:
		return false
	}
}

func newStreamResponseError(eventType string, envelope map[string]any) error {
	detail := transformerModel.ErrorDetail{Type: strings.TrimSpace(eventType)}
	errorObject := nestedMap(envelope, "error")
	if response := nestedMap(envelope, "response"); response != nil {
		if nested := nestedMap(response, "error"); nested != nil {
			errorObject = nested
		}
	}
	if errorObject != nil {
		if value := stringField(errorObject, "type"); value != "" {
			detail.Type = value
		}
		detail.Code = stringField(errorObject, "code")
		detail.Message = stringField(errorObject, "message")
	}
	if detail.Message == "" {
		detail.Message = "upstream stream error"
	}
	return &transformerModel.ResponseError{
		StatusCode: streamErrorStatus(detail.Type, detail.Code),
		Detail:     detail,
	}
}

func streamErrorStatus(errorType, code string) int {
	value := strings.ToLower(strings.TrimSpace(errorType + " " + code))
	switch {
	case strings.Contains(value, "authentication") || strings.Contains(value, "unauthorized"):
		return http.StatusUnauthorized
	case strings.Contains(value, "permission") || strings.Contains(value, "forbidden"):
		return http.StatusForbidden
	case strings.Contains(value, "rate_limit") || strings.Contains(value, "rate limit"):
		return http.StatusTooManyRequests
	case strings.Contains(value, "not_found") || strings.Contains(value, "not found"):
		return http.StatusNotFound
	case strings.Contains(value, "invalid_request") || strings.Contains(value, "invalid request"):
		return http.StatusBadRequest
	case strings.Contains(value, "overloaded"):
		return 529
	default:
		return http.StatusInternalServerError
	}
}

func nestedMap(object map[string]any, key string) map[string]any {
	if object == nil {
		return nil
	}
	value, _ := object[key].(map[string]any)
	return value
}

func stringField(object map[string]any, key string) string {
	if object == nil {
		return ""
	}
	value, _ := object[key].(string)
	return value
}

func hasMeaningfulValue(object map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := object[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return true
			}
		case []any:
			if len(typed) > 0 {
				return true
			}
		case map[string]any:
			if len(typed) > 0 {
				return true
			}
		default:
			if fmt.Sprint(typed) != "" {
				return true
			}
		}
	}
	return false
}
