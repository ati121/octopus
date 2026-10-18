package relay

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// recordSuccessfulWSAffinity pins the next turn of this response to the channel
// and key that produced it. AxonHub owns the upstream WebSocket pool, so
// Octopus only remembers which channel served the response; the library reuses
// its own connection when that channel is selected again.
func (ra *relayAttempt) recordSuccessfulWSAffinity(_ interface{}) {
	if ra == nil || ra.metrics == nil {
		return
	}
	ra.collectResponse()
	if ra.metrics.InternalResponse == nil {
		return
	}
	responseID := strings.TrimSpace(ra.metrics.InternalResponse.ID)
	if responseID == "" {
		return
	}
	ttl := wsAffinityTTL(ra.groupSessionTTL)
	if ra.apiKeyID <= 0 || ra.groupID <= 0 || strings.TrimSpace(ra.requestModel) == "" {
		return
	}
	scope := wsAffinityScope{
		APIKeyID:     ra.apiKeyID,
		GroupID:      ra.groupID,
		RequestModel: ra.requestModel,
		ResponseID:   responseID,
	}
	entry := wsAffinityEntry{
		ChannelID:     ra.channel.ID,
		ChannelKeyID:  ra.usedKey.ID,
		UpstreamModel: ra.internalRequest.Model,
	}
	ctx := ra.requestContext()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := getWSAffinityStore().Set(ctx, scope, entry, ttl); err != nil {
		log.Debugf("failed to persist ws response affinity (apikey=%d, group=%d, request_model=%s, response_id=%s): %v", ra.apiKeyID, ra.groupID, ra.requestModel, responseID, err)
	}
}
