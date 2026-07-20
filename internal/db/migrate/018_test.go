package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateStatsCacheReadTokens(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RelayLog{}, &model.StatsTotal{}, &model.StatsDaily{}, &model.StatsHourly{}); err != nil {
		t.Fatalf("AutoMigrate stats: %v", err)
	}

	now := time.Now()
	today := now.Format("20060102")
	yesterdayTime := now.Add(-24 * time.Hour)
	yesterday := yesterdayTime.Format("20060102")
	cache100, cache40 := 100, 40
	logs := []model.RelayLog{
		{ID: 1, Time: now.Unix(), CacheReadTokens: &cache100},
		{ID: 2, Time: yesterdayTime.Unix(), CacheReadTokens: &cache40},
		{ID: 3, Time: now.Unix()},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("create relay logs: %v", err)
	}
	if err := db.Create(&model.StatsTotal{ID: 1}).Error; err != nil {
		t.Fatalf("create total stats: %v", err)
	}
	if err := db.Create(&[]model.StatsDaily{{Date: today}, {Date: yesterday}}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}
	if err := db.Create(&model.StatsHourly{Hour: now.Hour(), Date: today}).Error; err != nil {
		t.Fatalf("create hourly stats: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := migrateStatsCacheReadTokens(db); err != nil {
			t.Fatalf("migrateStatsCacheReadTokens: %v", err)
		}
	}

	var total model.StatsTotal
	if err := db.First(&total, 1).Error; err != nil || total.CacheReadToken != 140 {
		t.Fatalf("total cache tokens: got %d, err=%v", total.CacheReadToken, err)
	}
	var todayStats model.StatsDaily
	if err := db.First(&todayStats, "date = ?", today).Error; err != nil || todayStats.CacheReadToken != 100 {
		t.Fatalf("today cache tokens: got %d, err=%v", todayStats.CacheReadToken, err)
	}
	var yesterdayStats model.StatsDaily
	if err := db.First(&yesterdayStats, "date = ?", yesterday).Error; err != nil || yesterdayStats.CacheReadToken != 40 {
		t.Fatalf("yesterday cache tokens: got %d, err=%v", yesterdayStats.CacheReadToken, err)
	}
	var hourly model.StatsHourly
	if err := db.First(&hourly, "hour = ?", now.Hour()).Error; err != nil || hourly.CacheReadToken != 100 {
		t.Fatalf("hourly cache tokens: got %d, err=%v", hourly.CacheReadToken, err)
	}
}
