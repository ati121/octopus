package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// Inbound 接收 TypeSafe System One 请求（POST /v1/systemone）。
// 请求体整体保留，只做必填字段的结构校验；question 类型、criteria 数量等
// 细节交给上游校验并返回 422 详情，避免上游新增 question 类型时被网关拦截。
type Inbound struct {
	storedResponse *model.InternalLLMResponse
}

type requestEnvelope struct {
	Model     string          `json:"model"`
	State     json.RawMessage `json:"state"`
	Questions json.RawMessage `json:"questions"`
}

func (i *Inbound) TransformRequest(_ context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var payload requestEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid system one request: %w", err)
	}
	if strings.TrimSpace(payload.Model) == "" {
		return nil, errors.New("model is required")
	}
	if !validSystemOneState(payload.State) {
		return nil, errors.New("state is required")
	}
	if !validSystemOneQuestions(payload.Questions) {
		return nil, errors.New("questions must be a non-empty object of question objects")
	}

	return &model.InternalLLMRequest{
		Model:            strings.TrimSpace(payload.Model),
		SystemOnePayload: append(json.RawMessage(nil), body...),
		RawAPIFormat:     model.APIFormatSystemOne,
	}, nil
}

// validSystemOneState 只要求 state 存在且不为 null；上游接受字符串、对象或数组。
func validSystemOneState(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func validSystemOneQuestions(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}

	var questions map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &questions); err != nil || len(questions) == 0 {
		return false
	}
	for _, question := range questions {
		value := bytes.TrimSpace(question)
		if len(value) == 0 || value[0] != '{' {
			return false
		}
	}
	return true
}

func (i *Inbound) TransformResponse(_ context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	if response == nil || len(response.SystemOnePayload) == 0 {
		return nil, errors.New("system one response is empty")
	}
	i.storedResponse = response
	return append([]byte(nil), response.SystemOnePayload...), nil
}

func (i *Inbound) TransformStream(context.Context, *model.InternalLLMResponse) ([]byte, error) {
	return nil, errors.New("streaming is not supported for system one API")
}

func (i *Inbound) GetInternalResponse(context.Context) (*model.InternalLLMResponse, error) {
	return i.storedResponse, nil
}
