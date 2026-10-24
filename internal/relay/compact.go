package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

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

	metricsReq := &transformerModel.InternalLLMRequest{Request: transformerModel.Request{Model: requestModel}, RawBody: body}
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

			statusCode, retryAfter, attemptErr = forwardResponsesCompact(c, metrics, iter, channel, usedKey, &transformerModel.InternalLLMRequest{Request: *compactReq})
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

func forwardResponsesCompact(c *gin.Context, metrics *RelayMetrics, iter *balancer.Iterator, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, request *transformerModel.InternalLLMRequest) (int, time.Duration, error) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	view := *request
	view.Model = iter.Item().ModelName
	attempt := &relayAttempt{relayRequest: &relayRequest{c: c, inAdapter: &axon.Inbound{Transformer: axonResponses.NewCompactInboundTransformer()}, internalRequest: &view, metrics: metrics}, channel: channel, usedKey: usedKey, outAdapter: axon.NewOutboundWithEndpointPath(int(channel.Type), channel.EndpointPath)}
	status, err := attempt.pipelineHTTP(c.Request.Context())
	if err != nil {
		span.End(dbmodel.AttemptFailed, status, err.Error())
	} else {
		span.End(dbmodel.AttemptSuccess, status, "")
	}
	return status, attempt.retryAfter, err
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
