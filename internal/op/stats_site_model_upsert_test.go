package op

import (
	"strings"
	"testing"

	"gorm.io/gorm/clause"
)

func siteModelExprSQL(t *testing.T, updates map[string]interface{}, key string) string {
	t.Helper()
	expr, ok := updates[key].(clause.Expr)
	if !ok {
		t.Fatalf("assignment %q is %T, want clause.Expr", key, updates[key])
	}
	return expr.SQL
}

func TestSiteModelHourlyConflictUpdatesPostgres(t *testing.T) {
	updates := siteModelHourlyConflictUpdatesForDialect("postgres")
	if got := siteModelExprSQL(t, updates, "date"); got != "EXCLUDED.date" {
		t.Fatalf("date expression: %q", got)
	}
	if got := siteModelExprSQL(t, updates, "last_request_at"); !strings.Contains(got, "GREATEST(") {
		t.Fatalf("last request expression: %q", got)
	}
}

func TestSiteModelHourlyConflictUpdatesMySQL(t *testing.T) {
	updates := siteModelHourlyConflictUpdatesForDialect("mysql")
	if got := siteModelExprSQL(t, updates, "date"); got != "VALUES(date)" {
		t.Fatalf("date expression: %q", got)
	}
	if got := siteModelExprSQL(t, updates, "input_token"); !strings.Contains(got, "VALUES(input_token)") {
		t.Fatalf("input token expression: %q", got)
	}
}

func TestSiteModelHourlyConflictUpdatesSQLite(t *testing.T) {
	updates := siteModelHourlyConflictUpdatesForDialect("sqlite")
	if got := siteModelExprSQL(t, updates, "last_request_at"); !strings.Contains(got, "MAX(") || strings.Contains(got, "GREATEST(") {
		t.Fatalf("last request expression: %q", got)
	}
}
