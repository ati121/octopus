package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
)

type Outbound struct{}

type responseUsageUnits struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	ImageTokens  int64 `json:"image_tokens"`
}

type responseEnvelope struct {
	ID      string          `json:"id"`
	Results json.RawMessage `json:"results"`
	Meta    struct {
		Tokens      *responseUsageUnits `json:"tokens"`
		BilledUnits *responseUsageUnits `json:"billed_units"`
	} `json:"meta"`
}

func (o *Outbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseURL, key string) (*http.Request, error) {
	if request == nil || !request.IsRerankRequest() {
		return nil, errors.New("not a rerank request")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(request.RerankPayload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rerank request: %w", err)
	}
	if payload == nil {
		return nil, errors.New("rerank request must be a JSON object")
	}
	encodedModel, err := json.Marshal(request.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank model: %w", err)
	}
	payload["model"] = encodedModel
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}

	parsedURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, errors.New("rerank base url must include scheme and host")
	}
	parsedURL.Path = strings.TrimRight(parsedURL.Path, "/") + "/rerank"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func (o *Outbound) TransformResponse(_ context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("rerank response is nil")
	}
	body, err := iolimit.ReadAll(response.Body, iolimit.UpstreamResponseMaxBytes())
	if err != nil {
		return nil, fmt.Errorf("failed to read rerank response body: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("rerank response body is empty")
	}

	var payload responseEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rerank response: %w", err)
	}
	trimmedResults := bytes.TrimSpace(payload.Results)
	if len(trimmedResults) == 0 {
		return nil, errors.New("rerank response is missing results")
	}
	if trimmedResults[0] != '[' {
		return nil, errors.New("rerank response results must be an array")
	}
	var results []json.RawMessage
	if err := json.Unmarshal(trimmedResults, &results); err != nil {
		return nil, fmt.Errorf("rerank response results must be an array: %w", err)
	}

	usageUnits := payload.Meta.Tokens
	if usageUnits == nil {
		usageUnits = payload.Meta.BilledUnits
	}

	return &model.InternalLLMResponse{
		ID:            payload.ID,
		RerankPayload: append(json.RawMessage(nil), body...),
		Usage:         rerankUsage(usageUnits),
	}, nil
}

func rerankUsage(units *responseUsageUnits) *model.Usage {
	if units == nil {
		return nil
	}
	promptTokens := units.InputTokens + units.ImageTokens
	return &model.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: units.OutputTokens,
		TotalTokens:      promptTokens + units.OutputTokens,
	}
}

func (o *Outbound) TransformStream(context.Context, []byte) (*model.InternalLLMResponse, error) {
	return nil, errors.New("streaming is not supported for rerank API")
}
