package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestChannelStatsMigrationKeepsHistoryWithoutForeignKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"PRAGMA foreign_keys = ON",
		"CREATE TABLE channels (id integer PRIMARY KEY AUTOINCREMENT, name text)",
		"INSERT INTO channels (id, name) VALUES (1, '历史渠道')",
		"CREATE TABLE stats_channels (channel_id integer PRIMARY KEY, input_token integer, output_token integer, CONSTRAINT fk_channels_stats FOREIGN KEY (channel_id) REFERENCES channels(id))",
		"INSERT INTO stats_channels (channel_id, input_token, output_token) VALUES (1, 12345, 678)",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&model.StatsChannel{}); err != nil {
		t.Fatal(err)
	}
	if err := backfillChannelStatsNames(db); err != nil {
		t.Fatal(err)
	}
	var before model.StatsChannel
	if err := db.First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillChannelStatsNames(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM channels WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	var after model.StatsChannel
	if err := db.First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if before != after || after.Name != "历史渠道" || after.InputToken != 12345 || after.OutputToken != 678 || after.HistoryID == "" {
		t.Fatalf("unexpected history: %+v -> %+v", before, after)
	}
}
