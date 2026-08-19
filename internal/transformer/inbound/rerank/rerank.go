package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

type Inbound struct {
	storedResponse *model.InternalLLMResponse
}

type requestEnvelope struct {
	Model     string          `json:"model"`
	Query     json.RawMessage `json:"query"`
	Documents json.RawMessage `json:"documents"`
}

func (i *Inbound) TransformRequest(_ context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var payload requestEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid rerank request: %w", err)
	}
	if strings.TrimSpace(payload.Model) == "" {
		return nil, errors.New("model is required")
	}
	if !validRerankQuery(payload.Query) {
		return nil, errors.New("query is required")
	}
	if !validRerankDocuments(payload.Documents) {
		return nil, errors.New("documents are required")
	}

	return &model.InternalLLMRequest{
		Model:         strings.TrimSpace(payload.Model),
		RerankPayload: append(json.RawMessage(nil), body...),
		RawAPIFormat:  model.APIFormatRerank,
	}, nil
}

func validRerankQuery(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return strings.TrimSpace(text) != ""
	}

	var object map[string]json.RawMessage
	return json.Unmarshal(trimmed, &object) == nil && len(object) > 0
}

func validRerankDocuments(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return strings.TrimSpace(text) != ""
	}

	var documents []json.RawMessage
	return json.Unmarshal(trimmed, &documents) == nil && len(documents) > 0
}

func (i *Inbound) TransformResponse(_ context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	if response == nil || len(response.RerankPayload) == 0 {
		return nil, errors.New("rerank response is empty")
	}
	i.storedResponse = response
	return append([]byte(nil), response.RerankPayload...), nil
}

func (i *Inbound) TransformStream(context.Context, *model.InternalLLMResponse) ([]byte, error) {
	return nil, errors.New("streaming is not supported for rerank API")
}

func (i *Inbound) GetInternalResponse(context.Context) (*model.InternalLLMResponse, error) {
	return i.storedResponse, nil
}
