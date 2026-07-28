package relay

import (
	"bytes"
	"sync"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

var responsesItemReferenceUnsupported sync.Map

func markResponsesItemReferenceUnsupported(channelID int) {
	responsesItemReferenceUnsupported.Store(channelID, struct{}{})
}

func responsesItemReferenceIsUnsupported(channelID int) bool {
	_, ok := responsesItemReferenceUnsupported.Load(channelID)
	return ok
}

func (ra *relayAttempt) applyResponsesItemReferenceCompatibility() func() {
	if ra == nil || ra.internalRequest == nil || ra.channel == nil ||
		ra.channel.Type != outbound.OutboundTypeOpenAIResponse {
		return func() {}
	}

	previous := ra.internalRequest.TransformOptions.OmitResponsesItemReference
	if responsesItemReferenceIsUnsupported(ra.channel.ID) {
		ra.internalRequest.TransformOptions.OmitResponsesItemReference = true
	}
	return func() {
		ra.internalRequest.TransformOptions.OmitResponsesItemReference = previous
	}
}

func (ra *relayAttempt) shouldRetryWithoutResponsesItemReference(statusCode int, body []byte) bool {
	if ra == nil || ra.internalRequest == nil || ra.channel == nil ||
		ra.channel.Type != outbound.OutboundTypeOpenAIResponse ||
		ra.internalRequest.TransformOptions.OmitResponsesItemReference ||
		statusCode != 400 {
		return false
	}

	normalized := bytes.ToLower(body)
	return bytes.Contains(normalized, []byte("item_reference")) &&
		(bytes.Contains(normalized, []byte("unknown parameter")) ||
			bytes.Contains(normalized, []byte("unknown field")) ||
			bytes.Contains(normalized, []byte("extra inputs")))
}
