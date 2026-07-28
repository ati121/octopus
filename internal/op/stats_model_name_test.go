package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestStatsModelNameIDStableAndNonZero(t *testing.T) {
	a := statsModelNameID("gpt-4o")
	b := statsModelNameID("GPT-4O")
	if a == 0 || a != b {
		t.Fatalf("expected stable non-zero id, got %d and %d", a, b)
	}
	if statsModelNameID("gpt-4o-mini") == a {
		t.Fatal("different common model names should not collide")
	}
}

func TestStatsModelNameUpdateAggregates(t *testing.T) {
	statsModelCache.Clear()
	statsModelCacheNeedUpdateLock.Lock()
	statsModelCacheNeedUpdate = make(map[int]struct{})
	statsModelCacheNeedUpdateLock.Unlock()

	_ = StatsModelNameUpdate("gpt-4o", 1, model.StatsMetrics{InputToken: 3, CacheReadToken: 1, RequestSuccess: 1})
	_ = StatsModelNameUpdate("gpt-4o", 2, model.StatsMetrics{InputToken: 2, CacheWriteToken: 2, RequestSuccess: 1})
	rows := StatsModelList()
	if len(rows) != 1 {
		t.Fatalf("expected one aggregate row, got %#v", rows)
	}
	if rows[0].InputToken != 5 || rows[0].CacheReadToken != 1 || rows[0].CacheWriteToken != 2 || rows[0].RequestSuccess != 2 {
		t.Fatalf("unexpected aggregate: %+v", rows[0])
	}
}
