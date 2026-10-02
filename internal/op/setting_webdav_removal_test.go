package op

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestWebDAVRemovalPreservesManualBackup(t *testing.T) {
	ctx := setupBackupTestDB(t)
	legacy := []model.Setting{
		{Key: "webdav_url", Value: "https://dav.example.com"},
		{Key: "webdav_username", Value: "test-user"},
		{Key: "webdav_password", Value: "test-only-password"},
		{Key: "webdav_backup_path", Value: "/test-backups"},
		{Key: "webdav_backup_interval", Value: "1"},
		{Key: "webdav_retention_count", Value: "10"},
		{Key: "webdav_include_stats", Value: "true"},
	}
	rows := append([]model.Setting{{Key: model.SettingKeyRelayRequestTimeout, Value: "120"}}, legacy...)
	if err := db.GetDB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range legacy {
		settingCache.Set(row.Key, row.Value)
	}
	t.Cleanup(settingCache.Clear)
	assertRemoved := func() {
		t.Helper()
		for _, row := range legacy {
			var count int64
			if err := db.GetDB().Model(&model.Setting{}).Where("key = ?", row.Key).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("obsolete setting %s remains: count=%d err=%v", row.Key, count, err)
			}
			if _, err := SettingGetString(row.Key); err == nil {
				t.Fatalf("obsolete setting remains in cache: %s", row.Key)
			}
		}
	}
	for i := 0; i < 2; i++ {
		if err := settingRefreshCache(ctx); err != nil {
			t.Fatal(err)
		}
		assertRemoved()
	}

	exported, err := DBExportAll(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range exported.Settings {
		for _, row := range legacy {
			if setting.Key == row.Key {
				t.Fatalf("obsolete setting exported: %s", setting.Key)
			}
		}
	}
	data, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	var imported model.DBDump
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatal(err)
	}
	// 模拟用户手动导入含有旧 WebDAV 配置的 JSON 备份。
	imported.Settings = append(imported.Settings, legacy...)
	before := append([]model.Setting(nil), imported.Settings...)
	if err := SettingSetString(model.SettingKeyRelayRequestTimeout, "300"); err != nil {
		t.Fatal(err)
	}
	if _, err := DBImportIncremental(ctx, &imported); err != nil {
		t.Fatal(err)
	}
	assertRemoved() // 刷新缓存前检查，确保导入本身已经过滤废弃字段。
	if !reflect.DeepEqual(imported.Settings, before) {
		t.Fatal("import mutated caller's backup")
	}
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := SettingGetString(model.SettingKeyRelayRequestTimeout); err != nil || got != "120" {
		t.Fatalf("manual backup did not restore active setting: value=%q err=%v", got, err)
	}
}
