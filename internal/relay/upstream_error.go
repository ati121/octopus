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
	}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if message := strings.TrimSpace(payload.Message); message != "" {
		return message
	}
	if len(payload.Error) == 0 {
		return ""
	}

	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(payload.Error, &detail) == nil {
		if message := strings.TrimSpace(detail.Message); message != "" {
			return message
		}
	}
	var message string
	if json.Unmarshal(payload.Error, &message) == nil {
		return strings.TrimSpace(message)
	}
	return ""
}

func publicRelayErrorMessage(err error) string {
	var upstreamErr *upstreamHTTPError
	if errors.As(err, &upstreamErr) && upstreamErr != nil && upstreamErr.message != "" {
		return upstreamErr.message
	}
	return "channel failed"
}
