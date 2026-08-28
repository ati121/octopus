package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBackfillSiteModelStateOverrides(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.SiteModel{}, &model.SiteModelStateOverride{}); err != nil {
		t.Fatalf("AutoMigrate site models: %v", err)
	}

	rows := []model.SiteModel{
		{SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-4.1", Disabled: true},
		{SiteAccountID: 1, GroupKey: "vip", ModelName: "claude-opus-5", Disabled: true},
		{SiteAccountID: 2, GroupKey: "default", ModelName: "gpt-4.1"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create site models: %v", err)
	}

	// 迁移必须幂等：失败重跑或多进程启动都不能撞唯一索引。
	for i := 0; i < 2; i++ {
		if err := backfillSiteModelStateOverrides(db); err != nil {
			t.Fatalf("backfillSiteModelStateOverrides run %d failed: %v", i+1, err)
		}
	}

	var overrides []model.SiteModelStateOverride
	if err := db.Order("site_account_id ASC, group_key ASC").Find(&overrides).Error; err != nil {
		t.Fatalf("query overrides: %v", err)
	}
	if len(overrides) != 2 {
		t.Fatalf("expected 2 backfilled overrides, got %+v", overrides)
	}
	for _, item := range overrides {
		if !item.Disabled {
			t.Fatalf("backfilled override must be disabled, got %+v", item)
		}
		if item.SiteAccountID != 1 {
			t.Fatalf("only disabled models must be backfilled, got %+v", item)
		}
	}
}
