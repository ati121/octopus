package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestSettingRefreshRemovesGatewaySearchAndPreservesHeartbeat(t *testing.T) {
	ctx := setupBackupTestDB(t)
	rows := []model.Setting{
		{Key: "web_search_enabled", Value: "1"},
		{Key: "web_search_max_rounds", Value: "10"},
		{Key: model.SettingKeySSEHeartbeatInterval, Value: "15"},
		{Key: model.SettingKeySSEPreStreamHeartbeatDelay, Value: "5"},
	}
	if err := db.GetDB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := settingRefreshCache(ctx); err != nil {
			t.Fatal(err)
		}
		for _, key := range removedGatewaySearchSettingKeys {
			if _, err := SettingGetString(key); err == nil {
				t.Fatalf("obsolete setting remains in cache: %s", key)
			}
		}
		var count int64
		if err := db.GetDB().Model(&model.Setting{}).Where("key IN ?", removedGatewaySearchSettingKeys).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("obsolete settings remain in database: count=%d err=%v", count, err)
		}
		for _, row := range rows[2:] {
			if got, err := SettingGetString(row.Key); err != nil || got != row.Value {
				t.Fatalf("heartbeat changed: %s=%q err=%v", row.Key, got, err)
			}
		}
	}
}

func TestImportIgnoresRemovedSearchSettings(t *testing.T) {
	ctx := setupBackupTestDB(t)
	dump := &model.DBDump{Settings: []model.Setting{
		{Key: "web_search_enabled", Value: "1"},
		{Key: "web_search_max_rounds", Value: "99"},
	}}
	result, err := DBImportIncremental(ctx, dump)
	if err != nil || result.RowsAffected["settings"] != 0 {
		t.Fatalf("obsolete-only import: result=%+v err=%v", result, err)
	}
	dump.Settings = append(dump.Settings, model.Setting{Key: model.SettingKeySSEHeartbeatInterval, Value: "20"})
	result, err = DBImportIncremental(ctx, dump)
	if err != nil || result.RowsAffected["settings"] != 1 {
		t.Fatalf("mixed import: result=%+v err=%v", result, err)
	}
	var rows []model.Setting
	if err := db.GetDB().Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Key != model.SettingKeySSEHeartbeatInterval || rows[0].Value != "20" {
		t.Fatalf("unexpected imported settings: %+v", rows)
	}
	if len(dump.Settings) != 3 || dump.Settings[0].Key != "web_search_enabled" {
		t.Fatal("import mutated caller's backup")
	}
}
