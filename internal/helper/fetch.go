package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/providercompat"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/dlclark/regexp2"
)

const (
	modelFetchUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
	maxModelFetchPages        = 100
	maxFetchedModels          = 10000
	maxFetchedModelNameBytes  = 4096
	maxFetchedModelNamesBytes = 8 * 1024 * 1024
)

type modelFetchAccumulator struct {
	models     []string
	totalBytes int
}

func newModelFetchAccumulator(capacity int) *modelFetchAccumulator {
	if capacity < 0 {
		capacity = 0
	}
	if capacity > maxFetchedModels {
		capacity = maxFetchedModels
	}
	return &modelFetchAccumulator{models: make([]string, 0, capacity)}
}

func (a *modelFetchAccumulator) Add(rawName string) error {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return nil
	}
	if len(name) > maxFetchedModelNameBytes {
		return fmt.Errorf("model name exceeds maximum size of %d bytes", maxFetchedModelNameBytes)
	}
	if len(a.models) >= maxFetchedModels {
		return fmt.Errorf("model list exceeds maximum count of %d", maxFetchedModels)
	}
	if len(name) > maxFetchedModelNamesBytes-a.totalBytes {
		return fmt.Errorf("model names exceed maximum total size of %d bytes", maxFetchedModelNamesBytes)
	}
	a.models = append(a.models, name)
	a.totalBytes += len(name)
	return nil
}

func FetchModels(ctx context.Context, request model.Channel) ([]string, error) {
	client, err := ChannelHTTPClientWithContext(ctx, &request)
	if err != nil {
		return nil, err
	}
	fetchModel := make([]string, 0)
	switch request.Type {
	case outbound.OutboundTypeAnthropic:
		fetchModel, err = fetchAnthropicModelsOrOpenAI(client, ctx, request)
	case outbound.OutboundTypeGemini:
		fetchModel, err = fetchGeminiModels(client, ctx, request)
	default:
		fetchModel, err = fetchOpenAIModels(client, ctx, request)
	}
	if err != nil {
		return nil, err
	}
	if request.MatchRegex != nil && *request.MatchRegex != "" {
		matchModel := make([]string, 0)
		re, err := regexp2.Compile(*request.MatchRegex, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		re.MatchTimeout = 200 * time.Millisecond
		for _, model := range fetchModel {
			matched, err := re.MatchString(model)
			if err != nil {
				return nil, err
			}
			if matched {
				matchModel = append(matchModel, model)
			}
		}
		return matchModel, nil
	}
	return fetchModel, nil
}

// refer: https://platform.openai.com/docs/api-reference/models/list
func fetchOpenAIModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(request.GetBaseUrl()), "/")
	urls := openAIModelListURLs(base)
	if len(urls) == 0 {
		return nil, fmt.Errorf("empty channel base url")
	}
	var lastErr error
	for _, modelsURL := range urls {
		models, err := fetchOpenAIModelsAt(client, ctx, request, modelsURL)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", modelsURL, err)
			continue
		}
		if len(models) == 0 {
			lastErr = fmt.Errorf("%s: empty model list", modelsURL)
			continue
		}
		return models, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no model list endpoint succeeded for %s", base)
}

func fetchOpenAIModelsAt(client *http.Client, ctx context.Context, request model.Channel, modelsURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	applyDefaultModelRequestHeaders(req, request)
	req.Header.Set("Authorization", "Bearer "+request.GetChannelKey().ChannelKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result model.OpenAIModelList
	if err := decodeModelJSONResponse(resp, &result); err != nil {
		return nil, err
	}

	models := newModelFetchAccumulator(len(result.Data))
	for _, m := range result.Data {
		if err := models.Add(m.ID); err != nil {
			return nil, err
		}
	}
	return models.models, nil
}

func openAIModelListURLs(baseURL string) []string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	lower := strings.ToLower(baseURL)
	urls := make([]string, 0, 4)
	add := func(url string) {
		for _, existing := range urls {
			if strings.EqualFold(existing, url) {
				return
			}
		}
		urls = append(urls, url)
	}
	if hasOpenAIVersionSuffix(lower) {
		add(baseURL + "/models")
		return urls
	}
	add(baseURL + "/models")
	add(baseURL + "/v1/models")
	add(baseURL + "/api/v1/models")
	add(baseURL + "/v1beta/models")
	return urls
}

func hasOpenAIVersionSuffix(baseURL string) bool {
	for _, suffix := range []string{"/v1", "/v1beta", "/api/v1", "/openai/v1", "/openai/v1beta"} {
		if strings.HasSuffix(baseURL, suffix) {
			return true
		}
	}
	return false
}

// refer: https://ai.google.dev/api/models
func fetchGeminiModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	allModels := newModelFetchAccumulator(128)
	pageToken := ""
	seenPageTokens := make(map[string]struct{})

	for page := 1; page <= maxModelFetchPages; page++ {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			request.GetBaseUrl()+"/models",
			nil,
		)
		if err != nil {
			return nil, err
		}
		applyDefaultModelRequestHeaders(req, request)
		req.Header.Set("X-Goog-Api-Key", request.GetChannelKey().ChannelKey)
		if pageToken != "" {
			q := req.URL.Query()
			q.Add("pageToken", pageToken)
			req.URL.RawQuery = q.Encode()
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		var result model.GeminiModelList
		if err := decodeModelJSONResponse(resp, &result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, m := range result.Models {
			name := strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")
			if err := allModels.Add(name); err != nil {
				return nil, err
			}
		}

		nextPageToken := strings.TrimSpace(result.NextPageToken)
		if nextPageToken == "" {
			break
		}
		if _, exists := seenPageTokens[nextPageToken]; exists {
			return nil, fmt.Errorf("gemini model pagination returned a repeated page token")
		}
		seenPageTokens[nextPageToken] = struct{}{}
		pageToken = nextPageToken
		if page == maxModelFetchPages {
			return nil, fmt.Errorf("gemini model pagination exceeds maximum of %d pages", maxModelFetchPages)
		}
	}
	if len(allModels.models) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels.models, nil
}

// refer: https://platform.claude.com/docs
func fetchAnthropicModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	allModels := newModelFetchAccumulator(128)
	var afterID string
	seenAfterIDs := make(map[string]struct{})
	for page := 1; page <= maxModelFetchPages; page++ {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			request.GetBaseUrl()+"/models",
			nil,
		)
		if err != nil {
			return nil, err
		}
		applyDefaultModelRequestHeaders(req, request)
		providercompat.SetAnthropicAuthHeader(req.Header, request.GetBaseUrl(), request.GetChannelKey().ChannelKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
		q := req.URL.Query()
		if afterID != "" {
			q.Set("after_id", afterID)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		var result model.AnthropicModelList
		if err := decodeModelJSONResponse(resp, &result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, m := range result.Data {
			if err := allModels.Add(m.ID); err != nil {
				return nil, err
			}
		}

		if !result.HasMore {
			break
		}

		nextAfterID := strings.TrimSpace(result.LastID)
		if nextAfterID == "" {
			return nil, fmt.Errorf("anthropic model pagination returned an empty last_id while has_more is true")
		}
		if _, exists := seenAfterIDs[nextAfterID]; exists {
			return nil, fmt.Errorf("anthropic model pagination returned a repeated last_id")
		}
		seenAfterIDs[nextAfterID] = struct{}{}
		afterID = nextAfterID
		if page == maxModelFetchPages {
			return nil, fmt.Errorf("anthropic model pagination exceeds maximum of %d pages", maxModelFetchPages)
		}
	}
	if len(allModels.models) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels.models, nil
}

func fetchAnthropicModelsOrOpenAI(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	models, err := fetchAnthropicModels(client, ctx, request)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	if err != nil {
		if fallback, fallbackErr := fetchOpenAIModels(client, ctx, request); fallbackErr == nil && len(fallback) > 0 {
			return fallback, nil
		} else if fallbackErr != nil {
			return nil, fmt.Errorf("anthropic models: %v; openai fallback: %w", err, fallbackErr)
		}
	}
	return models, err
}

func applyDefaultModelRequestHeaders(req *http.Request, request model.Channel) {
	if req == nil {
		return
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", modelFetchUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
	for _, header := range request.CustomHeader {
		if header.HeaderKey != "" {
			req.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

func decodeModelJSONResponse(resp *http.Response, result any) error {
	bodyBytes, err := iolimit.ReadAll(resp.Body, iolimit.MetadataResponseMaxBytes())
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return formatModelHTTPError(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes)
	}
	if len(bodyBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		if summary := extractModelHTMLResponseSummary(resp.Header.Get("Content-Type"), bodyBytes); summary != "" {
			return fmt.Errorf("decode response failed: %s", summary)
		}
		return fmt.Errorf("decode response failed: %w", err)
	}
	return nil
}

func formatModelHTTPError(statusCode int, contentType string, bodyBytes []byte) error {
	if payload, ok := parseModelErrorPayload(bodyBytes); ok {
		if message := extractModelErrorMessage(payload); message != "" {
			return fmt.Errorf("http %d: %s", statusCode, message)
		}
	}
	if summary := extractModelHTMLResponseSummary(contentType, bodyBytes); summary != "" {
		return fmt.Errorf("http %d: %s", statusCode, summary)
	}
	return fmt.Errorf("http %d: %s", statusCode, strings.TrimSpace(string(bodyBytes)))
}

func parseModelErrorPayload(bodyBytes []byte) (map[string]any, bool) {
	if len(bodyBytes) == 0 {
		return map[string]any{}, true
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractModelErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	if message, ok := payload["msg"].(string); ok && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	if errorPayload, ok := payload["error"].(map[string]any); ok {
		if message, ok := errorPayload["message"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
	}
	return ""
}

func extractModelHTMLResponseSummary(contentType string, bodyBytes []byte) string {
	body := strings.TrimSpace(string(bodyBytes))
	if body == "" {
		return ""
	}
	lowered := strings.ToLower(body)
	loweredContentType := strings.ToLower(contentType)
	if !strings.Contains(loweredContentType, "text/html") && !strings.Contains(lowered, "<html") && !strings.Contains(lowered, "<!doctype") {
		if strings.Contains(lowered, "just a moment") {
			return "Just a moment..."
		}
		return ""
	}
	if start := strings.Index(lowered, "<title>"); start >= 0 {
		start += len("<title>")
		if end := strings.Index(lowered[start:], "</title>"); end >= 0 {
			title := strings.TrimSpace(body[start : start+end])
			if pipe := strings.Index(title, "|"); pipe >= 0 {
				title = strings.TrimSpace(title[:pipe])
			}
			if title != "" {
				return title
			}
		}
	}
	if strings.Contains(lowered, "just a moment") {
		return "Just a moment..."
	}
	if strings.Contains(lowered, "cloudflare tunnel error") {
		return "Cloudflare Tunnel error"
	}
	if strings.Contains(lowered, "cloudflare") {
		return "Cloudflare challenge"
	}
	return ""
}
