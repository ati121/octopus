package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db/migrate"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func InitDB(dbType, dsn string, debug bool) error {
	var err error
	gormConfig := gorm.Config{Logger: logger.Discard}
	if debug {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	switch dbType {
	case "sqlite":
		db, err = initSQLite(dsn, &gormConfig)
	case "mysql":
		db, err = initMySQL(dsn, &gormConfig)
	case "postgres", "postgresql":
		db, err = initPostgres(dsn, &gormConfig)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	switch dbType {
	case "sqlite":
		// SQLite 单写模型：限制为单连接，避免连接池内自相竞争 SQLITE_BUSY；
		// WAL 模式下读连接由驱动内部处理，不会被该限制阻塞。
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(0)
		sqlDB.SetConnMaxIdleTime(0)
	default:
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetConnMaxLifetime(time.Hour)
		sqlDB.SetConnMaxIdleTime(10 * time.Minute)
	}

	if err := migrate.BeforeAutoMigrate(db); err != nil {
		return err
	}
	// RelayLog 仅作为进程内 DTO 使用，不再加入应用数据库 schema。
	// 旧版本创建的 relay_logs 会由一次性迁移删除并 VACUUM 回收空间。
	models := []interface{}{
		&model.User{},
		&model.Channel{},
		&model.ChannelKey{},
		&model.ProxyConfiguration{},
		&model.Site{},
		&model.SiteAccount{},
		&model.SiteToken{},
		&model.SiteUserGroup{},
		&model.SiteModel{},
		&model.SiteChannelBinding{},
		&model.Group{},
		&model.GroupItem{},
		&model.GroupPreset{},
		&model.LLMInfo{},
		&model.APIKey{},
		&model.Setting{},
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.StatsSiteModelHourly{},
		&model.GroupHealthSnapshot{},
		&model.GroupHealthAttempt{},
		&model.WSResponseAffinity{},
		&model.SiteChannelOutlierState{},
		&migrate.MigrationRecord{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	if err := migrate.AfterAutoMigrate(db); err != nil {
		return err
	}
	// Postgres: schema changes during migrations can invalidate cached prepared plans
	// (e.g. "cached plan must not change result type"). Clear them.
	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		db.Exec("DEALLOCATE ALL")
		db.Exec("DISCARD ALL")
	}
	return nil
}

func initSQLite(path string, config *gorm.Config) (*gorm.DB, error) {
	// glebarez/sqlite (modernc.org/sqlite) 只识别 _pragma=NAME(VALUE) 形式参数，
	// 旧的下划线参数会被静默忽略（导致 WAL/busy_timeout 实际未生效）。
	params := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(ON)",
		"_pragma=cache_size(-10000)",
		"_pragma=mmap_size(268435456)",
		"_pragma=temp_store(MEMORY)",
	}
	return gorm.Open(sqlite.Open(path+"?"+strings.Join(params, "&")), config)
}

func initMySQL(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local
	if !strings.Contains(dsn, "?") {
		dsn += "?charset=utf8mb4&parseTime=True&loc=Local"
	}
	return gorm.Open(mysql.Open(dsn), config)
}

func initPostgres(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: host=localhost user=postgres password=xxx dbname=octopus port=5432 sslmode=disable
	return gorm.Open(postgres.Open(dsn), config)
}

func Close() error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ensureRelayLogColumnsSQLite is kept for migration regression tests and for
// third-party callers that still inspect legacy databases. The application no
// longer invokes it because relay_logs is not part of the current schema.
func ensureRelayLogColumnsSQLite(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model.RelayLog{}); err != nil {
		return fmt.Errorf("parse relay_logs schema: %w", err)
	}
	for _, field := range stmt.Schema.Fields {
		if field.IgnoreMigration || field.DBName == "" {
			continue
		}
		var name string
		if err := db.Raw("SELECT name FROM pragma_table_info('relay_logs') WHERE name = ? LIMIT 1", field.DBName).Scan(&name).Error; err != nil {
			return fmt.Errorf("inspect relay_logs column %s: %w", field.DBName, err)
		}
		if name == field.DBName {
			continue
		}
		if err := db.Migrator().AddColumn(&model.RelayLog{}, field.Name); err != nil {
			return fmt.Errorf("add relay_logs column %s: %w", field.DBName, err)
		}
	}
	return nil
}

func GetDB() *gorm.DB {
	return db
}
