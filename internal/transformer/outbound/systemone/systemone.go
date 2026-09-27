package systemone

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

// Outbound 转发到 TypeSafe System One 接口（{baseURL}/systemone）。
// 请求体只替换实际路由的模型名；响应体原样保留，模型名取上游返回的
// 版本号（如 jev-1.13.0），用量按 input_tokens / output_tokens 记录。
type Outbound struct{}

type responseUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type responseEnvelope struct {
	Model   string          `json:"model"`
	Answers json.RawMessage `json:"answers"`
	Usage   *responseUsage  `json:"usage"`
}

func (o *Outbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseURL, key string) (*http.Request, error) {
	if request == nil || !request.IsSystemOneRequest() {
		return nil, errors.New("not a system one request")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(request.SystemOnePayload, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal system one request: %w", err)
	}
	if payload == nil {
		return nil, errors.New("system one request must be a JSON object")
	}
	encodedModel, err := json.Marshal(request.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal system one model: %w", err)
	}
	payload["model"] = encodedModel
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal system one request: %w", err)
	}

	parsedURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, errors.New("system one base url must include scheme and host")
	}
	parsedURL.Path = strings.TrimRight(parsedURL.Path, "/") + "/systemone"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create system one request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func (o *Outbound) TransformResponse(_ context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("system one response is nil")
	}
	body, err := iolimit.ReadAll(response.Body, iolimit.UpstreamResponseMaxBytes())
	if err != nil {
		return nil, fmt.Errorf("failed to read system one response body: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("system one response body is empty")
	}

	var payload responseEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal system one response: %w", err)
	}
	trimmedAnswers := bytes.TrimSpace(payload.Answers)
	if len(trimmedAnswers) == 0 || trimmedAnswers[0] != '{' {
		return nil, errors.New("system one response answers must be an object")
	}
	var answers map[string]json.RawMessage
	if err := json.Unmarshal(trimmedAnswers, &answers); err != nil {
		return nil, fmt.Errorf("system one response answers must be an object: %w", err)
	}
	if len(answers) == 0 {
		return nil, errors.New("system one response is missing answers")
	}

	return &model.InternalLLMResponse{
		Model:            strings.TrimSpace(payload.Model),
		SystemOnePayload: append(json.RawMessage(nil), body...),
		Usage:            systemOneUsage(payload.Usage),
	}, nil
}

func systemOneUsage(usage *responseUsage) *model.Usage {
	if usage == nil {
		return nil
	}
	return &model.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}
}

func (o *Outbound) TransformStream(context.Context, []byte) (*model.InternalLLMResponse, error) {
	return nil, errors.New("streaming is not supported for system one API")
}
