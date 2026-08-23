package migrate

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func init() {
	RegisterBeforeAutoMigration(Migration{
		Version: 21,
		Up:      removePersistedRelayLogs,
	})
}

// removePersistedRelayLogs removes the legacy relay_logs table.  Relay logs
// are now process-local (the newest 50 completed records are retained in
// memory), so keeping this table would only preserve the multi-gigabyte body
// archive that motivated the change.
//
// SQLite needs an explicit checkpoint and VACUUM after DROP TABLE: deleting a
// table frees pages for reuse but does not shrink data.db on disk when
// auto_vacuum is disabled.  VACUUM must run outside a transaction, which is
// why this migration deliberately uses individual statements.
func removePersistedRelayLogs(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	start := time.Now()
	dialect := ""
	if db.Dialector != nil {
		dialect = db.Dialector.Name()
	}
	hadTable := db.Migrator().HasTable("relay_logs")
	log.Infow("migration.relay_logs_memory.start",
		"dialect", dialect,
		"table_exists", hadTable,
	)
	if hadTable {
		if err := db.Exec("DROP TABLE relay_logs").Error; err != nil {
			return fmt.Errorf("drop legacy relay_logs table: %w", err)
		}
	}

	if dialect == "sqlite" {
		// Checkpoint any WAL pages produced by DROP TABLE before VACUUM.  SQLite
		// returns a result row for this pragma; Exec is sufficient and avoids
		// coupling the migration to a driver-specific result shape.
		if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
			return fmt.Errorf("checkpoint sqlite WAL after dropping relay_logs: %w", err)
		}
		log.Infow("migration.relay_logs_memory.vacuum.start")
		if err := db.Exec("VACUUM").Error; err != nil {
			return fmt.Errorf("vacuum sqlite database after dropping relay_logs: %w", err)
		}
	}

	log.Infow("migration.relay_logs_memory.done",
		"dialect", dialect,
		"table_dropped", hadTable,
		"duration", time.Since(start).String(),
	)
	return nil
}
