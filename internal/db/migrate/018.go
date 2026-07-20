package migrate

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateStatsCacheReadTokens,
	})
}

func migrateStatsCacheReadTokens(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.RelayLog{}) || !db.Migrator().HasColumn(&model.StatsTotal{}, "CacheReadToken") {
		return nil
	}

	type cacheLogRow struct {
		Time            int64
		CacheReadTokens int64
	}
	var rows []cacheLogRow
	if err := db.Model(&model.RelayLog{}).
		Select("time", "cache_read_tokens").
		Where("cache_read_tokens > 0").
		Find(&rows).Error; err != nil {
		return err
	}

	total := int64(0)
	daily := make(map[string]int64)
	hourly := make(map[int]int64)
	today := time.Now().Format("20060102")
	for _, row := range rows {
		t := time.Unix(row.Time, 0)
		date := t.Format("20060102")
		total += row.CacheReadTokens
		daily[date] += row.CacheReadTokens
		if date == today {
			hourly[t.Hour()] += row.CacheReadTokens
		}
	}

	if err := db.Model(&model.StatsTotal{}).Where("id = ?", 1).Update("cache_read_token", total).Error; err != nil {
		return err
	}
	for date, tokens := range daily {
		if err := db.Model(&model.StatsDaily{}).Where("date = ?", date).Update("cache_read_token", tokens).Error; err != nil {
			return err
		}
	}
	for hour, tokens := range hourly {
		if err := db.Model(&model.StatsHourly{}).Where("hour = ? AND date = ?", hour, today).Update("cache_read_token", tokens).Error; err != nil {
			return err
		}
	}
	return nil
}
