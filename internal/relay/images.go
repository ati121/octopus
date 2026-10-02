package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/axon"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

const imagesUpstreamErrorBodyLimit = 16 * 1024

// ImagesHandler 是 OpenAI Images API 的统一 relay 入口。
// endpoint 形如：/images/generations、/images/edits、/images/variations（不含 /v1 前缀）。
func ImagesHandler(endpoint string, c *gin.Context) {
	ctx := c.Request.Context()

	apiKeyID := c.GetInt("api_key_id")

	// 缓存请求体，支持多次重试重放
	bc, err := bodycache.New(c.Request.Body)
	if err != nil {
		var tooLarge *bodycache.BodyTooLargeError
		if errors.As(err, &tooLarge) {
			resp.Error(c, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() {
		if cerr := bc.Close(); cerr != nil {
			log.Warnf("failed to close images body cache: %v", cerr)
		}
	}()

	contentType := c.GetHeader("Content-Type")
	isMultipart := strings.Contains(strings.ToLower(contentType), "multipart/form-data")

	// The library owns image validation, including its non-streaming contract.
	imageIn, err := axon.ImageInbound(endpoint)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	imageReader, err := bc.NewReader()
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	imageBody, err := io.ReadAll(imageReader)
	_ = imageReader.Close()
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	native, err := imageIn.TransformRequest(ctx, &httpclient.Request{Method: "POST", Body: imageBody, ContentType: contentType, Headers: c.Request.Header.Clone()})
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	requestModel := native.Model
	var jsonPayload map[string]any
	if !isMultipart {
		_ = json.Unmarshal(imageBody, &jsonPayload)
	}

	// supported_models 校验（复用 APIKeyAuth 注入，支持白名单和黑名单）。
	if !apiKeyAllowsModel(c.GetString("supported_models"), c.GetString("model_list_mode"), requestModel) {
		resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
		return
	}

	// 获取通道分组
	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	// 初始化 Metrics（Images 独立，避免 b64_json 内存膨胀）
	metrics := newImagesRelayMetrics(apiKeyID, requestModel)
	metrics.RequestContent = buildImagesRequestContentForLog(isMultipart, bc, jsonPayload)
	metrics.StartLog()

	var lastErr error

	for iter.Next() {
		select {
		case <-ctx.Done():
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(ctx, false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()

		// 获取通道（Type 已按候选模型解析）
		channel, err := op.ChannelGetForModel(item.ChannelID, item.ModelName, ctx)
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		// channel.Type 限制：仅 OpenAI Chat/Responses
		if channel.Type != outbound.OutboundTypeOpenAIChat && channel.Type != outbound.OutboundTypeOpenAIResponse {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		usedKey := channel.GetChannelKey()
		if usedKey.ChannelKey == "" {
			iter.Skip(channel.ID, 0, channel.Name, "no available key")
			continue
		}

		// 熔断检查（熔断 key 使用 actualModel=item.ModelName）
		if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
			continue
		}

		log.Debugf("images request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

		span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)

		// 尝试一次转发
		statusCode, written, usage, upstreamCT, fwdErr := imagesAttempt(ctx, c, imageIn, native, channel, usedKey.ChannelKey, item.ModelName)

		// 更新 channel key 状态
		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()

		if fwdErr == nil {
			// ====== 成功 ======
			metrics.ActualModel = item.ModelName
			if usage != nil {
				metrics.SetUsageFromImages(item.ModelName, *usage)
			}
			metrics.ResponseContent = buildImagesResponseContentForLog(false, upstreamCT, usage)

			op.ChannelKeyUpdate(usedKey)

			span.End(model.AttemptSuccess, statusCode, "")

			// Channel 维度统计
			op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
				WaitTime:       span.Duration().Milliseconds(),
				RequestSuccess: 1,
			}, channel.Name)

			// 熔断器：记录成功
			balancer.RecordSuccess(channel.ID, usedKey.ID, item.ModelName)
			// 会话保持：更新粘性记录
			balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)

			metrics.SaveWithChannelStats(ctx, true, nil, iter.Attempts(), false)
			return
		}

		// ====== 失败 ======
		op.ChannelKeyUpdate(usedKey)
		span.End(model.AttemptFailed, statusCode, fwdErr.Error())

		// Channel 维度统计
		op.StatsChannelUpdate(channel.ID, model.StatsMetrics{
			WaitTime:      span.Duration().Milliseconds(),
			RequestFailed: 1,
		}, channel.Name)

		// 熔断器：记录失败
		balancer.RecordFailure(channel.ID, usedKey.ID, item.ModelName, circuitFailureKind(group.RetryEnabled, statusCode))

		if written {
			metrics.SaveWithChannelStats(ctx, false, fwdErr, iter.Attempts(), false)
			return
		}

		lastErr = fmt.Errorf("channel %s failed: %v", channel.Name, fwdErr)
	}

	// 所有通道都失败
	metrics.SaveWithChannelStats(ctx, false, lastErr, iter.Attempts(), false)
	resp.Error(c, http.StatusBadGateway, "all channels failed")
}

type imagesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type imagesRelayMetrics struct {
	APIKeyID     int
	RequestModel string
	ActualModel  string
	StartTime    time.Time
	LogID        int64
	FirstToken   time.Time

	Stats model.StatsMetrics

	RequestContent  string
	ResponseContent string
}

func newImagesRelayMetrics(apiKeyID int, requestModel string) *imagesRelayMetrics {
	return &imagesRelayMetrics{
		APIKeyID:     apiKeyID,
		RequestModel: requestModel,
		StartTime:    time.Now(),
	}
}

func (m *imagesRelayMetrics) StartLog() {
	if m.LogID != 0 {
		return
	}
	m.LogID = op.RelayLogStart(model.RelayLog{
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ActualModelName:  m.RequestModel,
	})
}

func (m *imagesRelayMetrics) SetFirstTokenTime(t time.Time) {
	if m.FirstToken.IsZero() {
		m.FirstToken = t
		if m.LogID != 0 {
			actualModel := m.ActualModel
			if actualModel == "" {
				actualModel = m.RequestModel
			}
			op.RelayLogProgress(model.RelayLog{
				ID:               m.LogID,
				Time:             m.StartTime.Unix(),
				RequestModelName: m.RequestModel,
				ActualModelName:  actualModel,
				Ftut:             int(m.FirstToken.Sub(m.StartTime).Milliseconds()),
			})
		}
	}
}

func (m *imagesRelayMetrics) SetUsageFromImages(actualModel string, u imagesUsage) {
	m.ActualModel = actualModel
	m.Stats.InputToken = int64(u.InputTokens)
	m.Stats.OutputToken = int64(u.OutputTokens)

}

func (m *imagesRelayMetrics) Save(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt) {
	m.SaveWithChannelStats(ctx, success, err, attempts, true)
}

func (m *imagesRelayMetrics) SaveWithChannelStats(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt, updateChannelStats bool) {
	duration := time.Since(m.StartTime)

	globalStats := model.StatsMetrics{
		WaitTime:    duration.Milliseconds(),
		InputToken:  m.Stats.InputToken,
		OutputToken: m.Stats.OutputToken,
	}
	if success {
		globalStats.RequestSuccess = 1
	} else {
		globalStats.RequestFailed = 1
	}

	attempts = enrichChannelAttemptProtocols(ctx, attempts)
	channelID, _ := finalChannel(attempts)
	logChannelID, logChannelName := finalLogChannel(attempts)
	protocol := finalChannelProtocol(attempts)
	op.StatsTotalUpdate(globalStats)
	op.StatsHourlyUpdate(globalStats)
	op.StatsDailyUpdate(context.Background(), globalStats)
	op.StatsAPIKeyUpdate(m.APIKeyID, globalStats)
	if updateChannelStats {
		op.StatsChannelUpdate(channelID, globalStats)
	} else {
		updateFinalChannelUsageStats(channelID, globalStats)
	}
	op.StatsSiteModelHourlyRecordAttempts(attempts, m.ActualModel)

	if conf.AppConfig.Log.Relay.Summary || !success {
		fields := []interface{}{
			"model", m.RequestModel,
			"actual_model", m.ActualModel,
			"channel_id", logChannelID,
			"channel", logChannelName,
			"protocol", protocol,
			"success", success,
			"duration_ms", duration.Milliseconds(),
			"input_token", m.Stats.InputToken,
			"output_token", m.Stats.OutputToken,
			"attempts", len(attempts),
		}
		if success {
			log.Infow("relay.images.complete", fields...)
		} else {
			log.Warnw("relay.images.complete", fields...)
		}
	}

	m.saveLog(ctx, success, err, duration, attempts, logChannelID, logChannelName)
}

func (m *imagesRelayMetrics) saveLog(ctx context.Context, success bool, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	relayLog := model.RelayLog{
		ID:               m.LogID,
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ChannelName:      channelName,
		Protocol:         finalChannelProtocol(attempts),
		ChannelId:        channelID,
		ActualModelName:  actualModel,
		UseTime:          int(duration.Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    len(attempts),
		RequestContent:   m.RequestContent,
		ResponseContent:  m.ResponseContent,
	}

	if apiKey, getErr := op.APIKeyGet(m.APIKeyID, ctx); getErr == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}

	// 首字时间
	if !m.FirstToken.IsZero() {
		relayLog.Ftut = int(m.FirstToken.Sub(m.StartTime).Milliseconds())
	}

	// Usage
	if m.Stats.InputToken > 0 || m.Stats.OutputToken > 0 {
		relayLog.InputTokens = int(m.Stats.InputToken)
		relayLog.OutputTokens = int(m.Stats.OutputToken)
	}

	if err != nil {
		relayLog.Error = err.Error()
	}
	relayLog.Success = success

	var logErr error
	if m.LogID != 0 {
		logErr = op.RelayLogUpdate(ctx, relayLog)
	} else {
		logErr = op.RelayLogAdd(ctx, relayLog)
	}
	if logErr != nil {
		log.Warnf("failed to finalize relay log: %v", logErr)
	}
}

func buildImagesRequestContentForLog(isMultipart bool, bc *bodycache.BodyCache, jsonPayload map[string]any) string {
	if isMultipart {
		// multipart 可能包含图片文件，避免把原始内容保存在日志内存中。
		return fmt.Sprintf(`{"content_type":"multipart/form-data","size_bytes":%d,"note":"multipart request content omitted for storage"}`, bc.Size())
	}
	if jsonPayload == nil {
		return ""
	}
	b, err := json.Marshal(jsonPayload)
	if err != nil {
		return ""
	}
	return truncateString(string(b), 8*1024)
}

func buildImagesResponseContentForLog(stream bool, upstreamCT string, usage *imagesUsage) string {
	if usage == nil {
		return ""
	}
	// 不记录 b64_json，仅记录 usage
	type respForLog struct {
		Stream      bool         `json:"stream"`
		ContentType string       `json:"content_type,omitempty"`
		Usage       *imagesUsage `json:"usage,omitempty"`
		Note        string       `json:"note,omitempty"`
	}
	obj := respForLog{
		Stream:      stream,
		ContentType: upstreamCT,
		Usage:       usage,
		Note:        "image data omitted for storage",
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(b)
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func imagesAttempt(
	ctx context.Context,
	c *gin.Context,
	in transformer.Inbound,
	native *llm.Request,
	channel *model.Channel,
	channelKey string,
	actualModel string,
) (statusCode int, written bool, usage *imagesUsage, upstreamCT string, err error) {
	out := axon.NewOutbound(int(channel.Type))
	req, err := out.TransformRequest(ctx, &transformerModel.InternalLLMRequest{Native: native, Model: actualModel}, channel.GetBaseUrl(), channelKey)
	if err != nil {
		return 0, false, nil, "", err
	}
	copyProxyHeaders(c.Request.Header, channel, req.Header)

	// 发送请求
	httpClient, err := helper.ChannelHTTPClientWithContext(ctx, channel)
	if err != nil {
		return 0, false, nil, "", err
	}

	respUp, err := httpClient.Do(req)
	if err != nil {
		return 0, false, nil, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer respUp.Body.Close()

	upstreamCT = respUp.Header.Get("Content-Type")

	if respUp.StatusCode < 200 || respUp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(respUp.Body, imagesUpstreamErrorBodyLimit))
		return respUp.StatusCode, false, nil, upstreamCT, fmt.Errorf("upstream error: %d: %s", respUp.StatusCode, string(b))
	}

	responseBody, err := iolimit.ReadAll(respUp.Body, iolimit.UpstreamResponseMaxBytes())
	if err != nil {
		return respUp.StatusCode, false, nil, upstreamCT, err
	}
	nativeResponse, err := out.Transformer.TransformResponse(ctx, &httpclient.Response{StatusCode: respUp.StatusCode, Headers: respUp.Header, Body: responseBody, Request: out.Request})
	if err != nil {
		return respUp.StatusCode, false, nil, upstreamCT, err
	}
	wire, err := in.TransformResponse(ctx, nativeResponse)
	if err != nil {
		return respUp.StatusCode, false, nil, upstreamCT, err
	}
	var u *imagesUsage
	if nativeResponse.Usage != nil {
		u = &imagesUsage{InputTokens: int(nativeResponse.Usage.PromptTokens), OutputTokens: int(nativeResponse.Usage.CompletionTokens), TotalTokens: int(nativeResponse.Usage.TotalTokens)}
	}
	c.Data(wire.StatusCode, "application/json", wire.Body)
	return respUp.StatusCode, c.Writer.Written(), u, upstreamCT, nil
}
