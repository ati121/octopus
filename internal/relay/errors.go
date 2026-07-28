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
