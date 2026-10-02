package migrate

import (
	"crypto/rand"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 24, Up: backfillChannelStatsNames})
}

// 渠道历史不再依赖配置表；升级时为已有统计保存名称。
func backfillChannelStatsNames(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.StatsChannel{}) || !db.Migrator().HasTable(&model.Channel{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasConstraint(&model.StatsChannel{}, "fk_channels_stats") {
			if err := tx.Migrator().DropConstraint(&model.StatsChannel{}, "fk_channels_stats"); err != nil {
				return err
			}
		}
		var channels []model.Channel
		if err := tx.Select("id", "name").Find(&channels).Error; err != nil {
			return err
		}
		for _, channel := range channels {
			if err := tx.Model(&model.StatsChannel{}).Where("channel_id = ? AND (name IS NULL OR name = '')", channel.ID).Update("name", channel.Name).Error; err != nil {
				return err
			}
		}
		var stats []model.StatsChannel
		if err := tx.Where("history_id IS NULL OR history_id = ''").Find(&stats).Error; err != nil {
			return err
		}
		for _, row := range stats {
			if err := tx.Model(&model.StatsChannel{}).Where("channel_id = ?", row.ChannelID).Update("history_id", rand.Text()).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.Setting{}) {
			return tx.Where("key = ?", "model_info_update_interval").Delete(&model.Setting{}).Error
		}
		return nil
	})
}
