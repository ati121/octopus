package relay

import "errors"

const (
	CodeRelayModelNotSupported     = "relay.model_not_supported"
	CodeRelayModelNotFound         = "relay.model_not_found"
	CodeRelayNoAvailableChannel    = "relay.no_available_channel"
	CodeRelayChannelDisabled       = "relay.channel_disabled"
	CodeRelayNoAvailableKey        = "relay.no_available_key"
	CodeRelayUpstreamFailed        = "relay.upstream_failed"
	CodeRelayTimeout               = "relay.timeout"
	CodeRelayCircuitBreakerTripped = "relay.circuit_breaker_tripped"
)

// ErrEmptyUpstreamResponse marks a successful HTTP response with no usable
// completion payload. It is retryable so the relay can fail over before writing
// a blank response to the client.
var ErrEmptyUpstreamResponse = errors.New("upstream returned empty response body")

// errRelayNoAvailableChannel 记录「分组下没有任何可用渠道」这个终态。
var errRelayNoAvailableChannel = errors.New("no available channel")

// errRelayAborted 是兜底日志的错误信息：请求在写入终态前就结束了（提前返回或 panic）。
var errRelayAborted = errors.New("relay aborted before completion")
