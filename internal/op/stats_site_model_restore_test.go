package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestRestoreSiteModelHourlyRowsMergesConcurrentUpdates(t *testing.T) {
	key := siteModelHourlyKey{Hour: 1, SiteAccountID: 2, GroupKey: "default", ModelName: "model-a"}
	siteModelHourlyCacheLock.Lock()
	siteModelHourlyCache = map[siteModelHourlyKey]*model.StatsSiteModelHourly{
		key: {
			Hour:          1,
			SiteAccountID: 2,
			GroupKey:      "default",
			ModelName:     "model-a",
			LastRequestAt: 20,
			StatsMetrics:  model.StatsMetrics{RequestSuccess: 1},
		},
	}
	siteModelHourlyCacheLock.Unlock()
	t.Cleanup(func() {
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
		siteModelHourlyCacheLock.Unlock()
	})

	restoreSiteModelHourlyRows([]model.StatsSiteModelHourly{{
		Hour:          1,
		SiteAccountID: 2,
		GroupKey:      "default",
		ModelName:     "model-a",
		LastRequestAt: 10,
		StatsMetrics:  model.StatsMetrics{RequestFailed: 2},
	}})

	siteModelHourlyCacheLock.Lock()
	got := *siteModelHourlyCache[key]
	siteModelHourlyCacheLock.Unlock()
	if got.RequestSuccess != 1 || got.RequestFailed != 2 || got.LastRequestAt != 20 {
		t.Fatalf("unexpected restored metrics: %#v", got)
	}
}
