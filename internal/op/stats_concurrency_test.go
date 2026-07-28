package op

import (
	"sync"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestStatsUpdatesAreAtomic(t *testing.T) {
	const (
		workers   = 200
		channelID = 880001
		modelID   = 880002
		apiKeyID  = 880003
	)
	statsChannelCache.Del(channelID)
	statsModelCache.Del(modelID)
	statsAPIKeyCache.Del(apiKeyID)
	t.Cleanup(func() {
		statsChannelCache.Del(channelID)
		statsModelCache.Del(modelID)
		statsAPIKeyCache.Del(apiKeyID)
	})

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			metrics := model.StatsMetrics{InputToken: 1}
			_ = StatsChannelUpdate(channelID, metrics)
			_ = StatsModelUpdate(model.StatsModel{ID: modelID, Name: "atomic-model", StatsMetrics: metrics})
			_ = StatsAPIKeyUpdate(apiKeyID, metrics)
		}()
	}
	wg.Wait()

	channel, _ := statsChannelCache.Get(channelID)
	modelStats, _ := statsModelCache.Get(modelID)
	apiKey, _ := statsAPIKeyCache.Get(apiKeyID)
	if channel.InputToken != workers || modelStats.InputToken != workers || apiKey.InputToken != workers {
		t.Fatalf("lost updates: channel=%d model=%d api_key=%d", channel.InputToken, modelStats.InputToken, apiKey.InputToken)
	}
}
