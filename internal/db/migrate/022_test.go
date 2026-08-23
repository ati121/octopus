package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCompactSQLiteDatabaseTruncatesMainFileAfterVacuum(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "compact.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_pragma=journal_mode(WAL)&_pragma=auto_vacuum(none)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Exec("CREATE TABLE payload (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
		t.Fatalf("create payload: %v", err)
	}
	large := make([]byte, 128*1024)
	for i := 1; i <= 40; i++ {
		if err := db.Exec("INSERT INTO payload(id, body) VALUES (?, ?)", i, large).Error; err != nil {
			t.Fatalf("insert payload %d: %v", i, err)
		}
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		t.Fatalf("checkpoint seeded WAL: %v", err)
	}
	if err := db.Exec("DROP TABLE payload").Error; err != nil {
		t.Fatalf("drop payload: %v", err)
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		t.Fatalf("checkpoint dropped payload: %v", err)
	}
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	if before.Size() < 1<<20 {
		t.Fatalf("test database did not grow enough: %d", before.Size())
	}

	if err := compactSQLiteDatabase(db); err != nil {
		t.Fatalf("compactSQLiteDatabase failed: %v", err)
	}
	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("main SQLite file was not truncated: before=%d after=%d", before.Size(), after.Size())
	}
	if after.Size() > 1<<20 {
		t.Fatalf("compacted SQLite file unexpectedly large: %d", after.Size())
	}
}
