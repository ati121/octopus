package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type upstreamHTTPError struct {
	status  int
	body    string
	message string
}

func newUpstreamHTTPError(status int, body []byte) *upstreamHTTPError {
	trimmed := strings.TrimSpace(string(body))
	return &upstreamHTTPError{
		status:  status,
		body:    trimmed,
		message: extractUpstreamErrorMessage(body),
	}
}

func (e *upstreamHTTPError) Error() string {
	if e == nil {
		return "upstream error"
	}
	if e.body == "" {
		return fmt.Sprintf("upstream error: %d", e.status)
	}
	return fmt.Sprintf("upstream error: %d: %s", e.status, e.body)
}

func extractUpstreamErrorMessage(body []byte) string {
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
		Detail  json.RawMessage `json:"detail"`
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if message := strings.TrimSpace(payload.Message); message != "" {
		return message
	}
	if message := extractErrorFieldMessage(payload.Error); message != "" {
		return message
	}
	return extractDetailFieldMessage(payload.Detail)
}

func extractErrorFieldMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &detail) == nil {
		if message := strings.TrimSpace(detail.Message); message != "" {
			return message
		}
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return strings.TrimSpace(message)
	}
	return ""
}

// extractDetailFieldMessage 解析 FastAPI / Pydantic 风格的错误（TypeSafe 等上游使用）：
// {"detail":"Not authenticated"} 或
// {"detail":[{"loc":["body","questions"],"msg":"Field required","type":"missing"}]}。
func extractDetailFieldMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var message string
	if json.Unmarshal(raw, &message) == nil {
		return strings.TrimSpace(message)
	}

	var items []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	messages := make([]string, 0, len(items))
	for _, item := range items {
		msg := strings.TrimSpace(item.Msg)
		if msg == "" {
			continue
		}
		loc := make([]string, 0, len(item.Loc))
		for _, part := range item.Loc {
			loc = append(loc, fmt.Sprint(part))
		}
		if len(loc) > 0 {
			msg = strings.Join(loc, ".") + ": " + msg
		}
		messages = append(messages, msg)
	}
	return strings.Join(messages, "; ")
}

func publicRelayErrorMessage(err error) string {
	var upstreamErr *upstreamHTTPError
	if errors.As(err, &upstreamErr) && upstreamErr != nil && upstreamErr.message != "" {
		return upstreamErr.message
	}
	return "channel failed"
}
