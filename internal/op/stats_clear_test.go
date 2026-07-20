package op

import (
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestStatsClearResetsAggregatesAndPreservesLogs(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	dbConn := dbpkg.GetDB().WithContext(ctx)
	today := time.Now().Format("20060102")
	metrics := model.StatsMetrics{InputToken: 100, OutputToken: 20, CacheReadToken: 80, RequestSuccess: 1}
	if err := dbConn.Create(&model.Channel{ID: 1, Name: "test-channel"}).Error; err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	if err := dbConn.Create(&model.APIKey{ID: 1, Name: "test-key", APIKey: "test-key"}).Error; err != nil {
		t.Fatalf("create api key failed: %v", err)
	}

	rows := []interface{}{
		&model.StatsTotal{ID: 1, StatsMetrics: metrics},
		&model.StatsDaily{Date: today, StatsMetrics: metrics},
		&model.StatsHourly{Hour: time.Now().Hour(), Date: today, StatsMetrics: metrics},
		&model.StatsChannel{ChannelID: 1, StatsMetrics: metrics},
		&model.StatsModel{ID: 1, Name: "test", ChannelID: 1, StatsMetrics: metrics},
		&model.StatsAPIKey{APIKeyID: 1, StatsMetrics: metrics},
		&model.StatsSiteModelHourly{Hour: int(time.Now().Unix() / 3600), SiteAccountID: 1, GroupKey: "default", ModelName: "test", Date: today, StatsMetrics: metrics},
		&model.RelayLog{ID: 1, Time: time.Now().Unix(), InputTokens: 100, CacheReadTokens: statsClearIntPtr(80)},
	}
	for _, row := range rows {
		if err := dbConn.Create(row).Error; err != nil {
			t.Fatalf("create %T failed: %v", row, err)
		}
	}
	if err := statsRefreshCache(ctx); err != nil {
		t.Fatalf("refresh stats cache failed: %v", err)
	}

	if err := StatsClear(ctx); err != nil {
		t.Fatalf("StatsClear failed: %v", err)
	}

	if total := StatsTotalGet(); total.ID != 1 || total.StatsMetrics != (model.StatsMetrics{}) {
		t.Fatalf("unexpected total after clear: %+v", total)
	}
	if todayStats := StatsTodayGet(); todayStats.Date != today || todayStats.StatsMetrics != (model.StatsMetrics{}) {
		t.Fatalf("unexpected today stats after clear: %+v", todayStats)
	}

	for name, value := range map[string]interface{}{
		"daily": &model.StatsDaily{}, "hourly": &model.StatsHourly{}, "channel": &model.StatsChannel{},
		"model": &model.StatsModel{}, "api key": &model.StatsAPIKey{}, "site model hourly": &model.StatsSiteModelHourly{},
	} {
		var count int64
		if err := dbConn.Model(value).Count(&count).Error; err != nil {
			t.Fatalf("count %s failed: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("expected %s stats to be empty, got %d", name, count)
		}
	}
	var logCount int64
	if err := dbConn.Model(&model.RelayLog{}).Count(&logCount).Error; err != nil {
		t.Fatalf("count relay logs failed: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("expected relay logs to be preserved, got %d", logCount)
	}
}

func statsClearIntPtr(value int) *int {
	return &value
}
