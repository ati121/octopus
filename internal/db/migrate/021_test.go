package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRemovePersistedRelayLogsDropsTableAndVacuumShrinksSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_pragma=journal_mode(WAL)&_pragma=auto_vacuum(none)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Exec("CREATE TABLE relay_logs (id INTEGER PRIMARY KEY, request_content TEXT, response_content TEXT)").Error; err != nil {
		t.Fatalf("create relay_logs: %v", err)
	}
	large := make([]byte, 128*1024)
	for i := 1; i <= 40; i++ {
		if err := db.Exec("INSERT INTO relay_logs(id, request_content, response_content) VALUES (?, ?, ?)", i, large, large).Error; err != nil {
			t.Fatalf("seed relay_logs row %d: %v", i, err)
		}
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		t.Fatalf("checkpoint seeded WAL: %v", err)
	}
	var beforePages, afterPages int64
	if err := db.Raw("PRAGMA page_count").Scan(&beforePages).Error; err != nil {
		t.Fatalf("page count before: %v", err)
	}
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	if before.Size() < 1<<20 {
		t.Fatalf("test database did not grow enough to exercise VACUUM: %d", before.Size())
	}

	if err := removePersistedRelayLogs(db); err != nil {
		t.Fatalf("removePersistedRelayLogs failed: %v", err)
	}
	if db.Migrator().HasTable("relay_logs") {
		t.Fatal("relay_logs table still exists after migration")
	}
	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if err := db.Raw("PRAGMA page_count").Scan(&afterPages).Error; err != nil {
		t.Fatalf("page count after: %v", err)
	}
	t.Logf("database size/pages before=%d/%d after=%d/%d", before.Size(), beforePages, after.Size(), afterPages)
	if afterPages >= beforePages {
		t.Fatalf("VACUUM did not reduce SQLite page count: before=%d after=%d", beforePages, afterPages)
	}

	// Idempotence: a second run must succeed even though the table is gone.
	if err := removePersistedRelayLogs(db); err != nil {
		t.Fatalf("second removePersistedRelayLogs failed: %v", err)
	}
}
