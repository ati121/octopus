package migrate

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 23,
		Up:      backfillSiteModelStateOverrides,
	})
}

// backfillSiteModelStateOverrides 把已有的 site_models.disabled = 1 迁移成显式的
// 用户表态记录。
//
// 在这次改动之前，手动停用只写在 site_models.disabled 上，而该表的行会在每轮同步中
// 被整体删除后重建，所以模型缺席一轮就会丢掉停用意图。site_model_state_overrides 是
// 新的唯一事实来源，必须把库里已有的停用意图搬进去，否则升级后第一轮同步就会把它们
// 全部重新启用。
//
// 表由 AutoMigrate 建好，因此注册为 AfterAutoMigrate。
func backfillSiteModelStateOverrides(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteModel{}) || !db.Migrator().HasTable(&model.SiteModelStateOverride{}) {
		return nil
	}

	start := time.Now()
	return db.Transaction(func(tx *gorm.DB) error {
		var disabled []model.SiteModel
		if err := tx.Where("disabled = ?", true).Find(&disabled).Error; err != nil {
			return fmt.Errorf("load manually disabled site models: %w", err)
		}
		if len(disabled) == 0 {
			log.Infow("migration.site_model_state_overrides.done", "backfilled", 0)
			return nil
		}

		var existing []model.SiteModelStateOverride
		if err := tx.Find(&existing).Error; err != nil {
			return fmt.Errorf("load existing site model overrides: %w", err)
		}
		seen := make(map[string]struct{}, len(existing))
		for _, item := range existing {
			seen[model.SiteModelOverrideKey(item.SiteAccountID, item.GroupKey, item.ModelName)] = struct{}{}
		}

		now := time.Now()
		rows := make([]model.SiteModelStateOverride, 0, len(disabled))
		for _, item := range disabled {
			key := model.SiteModelOverrideKey(item.SiteAccountID, item.GroupKey, item.ModelName)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rows = append(rows, model.SiteModelStateOverride{
				SiteAccountID: item.SiteAccountID,
				GroupKey:      model.NormalizeSiteGroupKey(item.GroupKey),
				ModelName:     item.ModelName,
				Disabled:      true,
				UpdatedAt:     now,
			})
		}
		if len(rows) == 0 {
			log.Infow("migration.site_model_state_overrides.done", "backfilled", 0)
			return nil
		}
		if err := tx.CreateInBatches(&rows, 200).Error; err != nil {
			return fmt.Errorf("backfill site model overrides: %w", err)
		}

		log.Infow("migration.site_model_state_overrides.done",
			"backfilled", len(rows),
			"duration", time.Since(start).String(),
		)
		return nil
	})
}
