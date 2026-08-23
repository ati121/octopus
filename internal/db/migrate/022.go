package migrate

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func init() {
	RegisterBeforeAutoMigration(Migration{
		Version: 22,
		Up:      compactSQLiteDatabase,
	})
}

// compactSQLiteDatabase repairs installations that already recorded migration
// 021 before its WAL checkpoint-after-VACUUM fix was added. In WAL mode SQLite
// may keep the compacted image in data.db-wal until a second checkpoint, so a
// successful VACUUM alone is not proof that the on-disk file has shrunk.
func compactSQLiteDatabase(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	dialect := ""
	if db.Dialector != nil {
		dialect = db.Dialector.Name()
	}
	if dialect != "sqlite" {
		return nil
	}

	start := time.Now()
	log.Infow("migration.sqlite_compact.start")
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return fmt.Errorf("checkpoint sqlite WAL before compacting: %w", err)
	}
	if err := db.Exec("VACUUM").Error; err != nil {
		return fmt.Errorf("vacuum sqlite database: %w", err)
	}
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return fmt.Errorf("checkpoint sqlite WAL after compacting: %w", err)
	}
	log.Infow("migration.sqlite_compact.done", "duration", time.Since(start).String())
	return nil
}
