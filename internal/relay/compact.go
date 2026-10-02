package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/axon"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/llm/httpclient"
	axonResponses "github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// HandleResponsesCompact proxies OpenAI-compatible /responses/compact requests upstream.
func HandleResponsesCompact(c *gin.Context) {
	body, err := iolimit.ReadRequestBody(c.Writer, c.Request, iolimit.RequestBodyMaxBytes())
	if err != nil {
		if iolimit.IsTooLarge(err) {
			resp.Error(c, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			resp.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	compactReq, err := axonResponses.NewCompactInboundTransformer().TransformRequest(c.Request.Context(), &httpclient.Request{Method: "POST", Body: body, ContentType: "application/json", Headers: c.Request.Header.Clone()})
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if !apiKeyAllowsModel(c.GetString("supported_models"), c.GetString("model_list_mode"), compactReq.Model) {
		resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
		return
	}

	requestModel := compactReq.Model
	apiKeyID := c.GetInt("api_key_id")

	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	metricsReq := &transformerModel.InternalLLMRequest{Model: requestModel, RawRequest: body}
	metrics := NewRelayMetrics(apiKeyID, requestModel, body, metricsReq)

	var lastErr error
	var lastStatusCode int
	var lastRetryAfter time.Duration

	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("compact request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()
		channel, err := op.ChannelGetForModel(item.ChannelID, item.ModelName, c.Request.Context())
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		if !supportsResponsesCompact(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with responses compact")
			continue
		}

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: iter.StickyKeyID(),
		}
		var usedKey dbmodel.ChannelKey
		for {
			usedKey = channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
			selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
			usedKey = dbmodel.ChannelKey{}
		}
		if usedKey.ChannelKey == "" {
			if len(selectOpts.ExcludeKeyIDs) == 0 {
				iter.Skip(channel.ID, 0, channel.Name, "no available key")
			}
			continue
		}

		var attemptErr error
		var statusCode int
		var retryAfter time.Duration
		var success bool

		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			if retryNum > 0 {
				delay := computeBackoff(retryNum, retryAfter)
				select {
				case <-c.Request.Context().Done():
					metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
					return
				case <-time.After(delay):
				}
			}

			statusCode, retryAfter, attemptErr = forwardResponsesCompact(c, metrics, iter, channel, usedKey, body)
			if attemptErr == nil {
				success = true
				break
			}
			if !isRetryableStatus(statusCode) {
				break
			}
		}

		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()
		op.ChannelKeyUpdate(usedKey)

		if success {
			op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestSuccess: 1}, channel.Name)
			balancer.RecordSuccess(channel.ID, usedKey.ID, requestModel)
			balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
			outlierwindow.Report(channel.ID, true, statusCode, time.Now())
			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}

		op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestFailed: 1}, channel.Name)
		failureKind := circuitFailureKind(group.RetryEnabled, statusCode)
		balancer.RecordFailure(channel.ID, usedKey.ID, requestModel, failureKind)
		outlierwindow.Report(channel.ID, false, statusCode, time.Now())
		lastErr = attemptErr
		lastStatusCode = statusCode
		lastRetryAfter = retryAfter
	}

	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)
	if lastErr == nil && lastStatusCode == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	if isPassthroughStatus(lastStatusCode) {
		if lastRetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastRetryAfter.Seconds())))
		}
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	if lastStatusCode > 0 {
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	resp.Error(c, http.StatusBadGateway, "channel failed")
}

func supportsResponsesCompact(channelType outbound.OutboundType) bool {
	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return true
	default:
		return false
	}
}

func forwardResponsesCompact(c *gin.Context, metrics *RelayMetrics, iter *balancer.Iterator, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, requestBody []byte) (int, time.Duration, error) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	request, err := buildResponsesCompactRequest(c.Request.Context(), channel, usedKey.ChannelKey, iter.Item().ModelName, requestBody)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to create compact request: %w", err)
	}
	metrics.SetTransportRequestPayload(requestBody, iter.Item().ModelName)
	copyProxyHeaders(c.Request.Header, channel, request.Header)

	response, err := sendCompactRequest(channel, request)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to send compact request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, truncated, readErr := iolimit.ReadAtMost(response.Body, iolimit.DefaultErrorBodyMaxBytes)
		if readErr != nil {
			span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
			return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr)
		}
		if truncated {
			body = append(body, []byte("\n[upstream error body truncated]")...)
		}
		retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		span.End(dbmodel.AttemptFailed, statusCode, string(body))
		return statusCode, retryAfter, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	body, readErr := iolimit.ReadAll(response.Body, iolimit.UpstreamResponseMaxBytes())
	if readErr != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
		return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr)
	}

	out, err := axonResponses.NewOutboundTransformer(channel.GetBaseUrl(), usedKey.ChannelKey)
	if err != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, err.Error())
		return response.StatusCode, 0, err
	}
	native, err := out.TransformResponse(c.Request.Context(), &httpclient.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: body, Request: &httpclient.Request{RequestType: "compact"}})
	if err != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, err.Error())
		return response.StatusCode, 0, err
	}
	wire, err := axonResponses.NewCompactInboundTransformer().TransformResponse(c.Request.Context(), native)
	if err != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, err.Error())
		return response.StatusCode, 0, err
	}
	copyProxyResponseHeaders(c.Writer.Header(), response.Header)
	c.Data(wire.StatusCode, "application/json", wire.Body)
	if view, err := axon.ProjectResponse(native); err == nil {
		metrics.SetInternalResponse(view, iter.Item().ModelName)
	}

	span.End(dbmodel.AttemptSuccess, response.StatusCode, "")
	return response.StatusCode, 0, nil
}

func buildResponsesCompactRequest(ctx context.Context, channel *dbmodel.Channel, key, actualModel string, requestBody []byte) (*http.Request, error) {
	in := axonResponses.NewCompactInboundTransformer()
	native, err := in.TransformRequest(ctx, &httpclient.Request{Method: "POST", Body: requestBody, ContentType: "application/json", Headers: make(http.Header)})
	if err != nil {
		return nil, err
	}
	native.Model = actualModel
	out, err := axonResponses.NewOutboundTransformer(channel.GetBaseUrl(), key)
	if err != nil {
		return nil, err
	}
	wire, err := out.TransformRequest(ctx, native)
	if err != nil {
		return nil, err
	}
	return axon.HTTPRequest(ctx, wire)
}

func copyProxyHeaders(src http.Header, channel *dbmodel.Channel, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		if hopByHopHeaders[lowerKey] || lowerKey == "content-type" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	for _, header := range channel.CustomHeader {
		if strings.EqualFold(header.HeaderKey, "Content-Type") {
			continue
		}
		dst.Set(header.HeaderKey, header.HeaderValue)
	}
	// 防止 Go 默认 User-Agent 泄露到上游
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "")
	}
}

func copyProxyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sendCompactRequest(channel *dbmodel.Channel, req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), channel)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}
