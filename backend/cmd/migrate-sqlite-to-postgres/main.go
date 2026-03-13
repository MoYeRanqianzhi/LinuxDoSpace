package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"linuxdospace/backend/internal/storage/postgres"
	_ "modernc.org/sqlite"
)

// tableCopyPlan describes one table that should be copied from SQLite into
// PostgreSQL together with the ordered columns to preserve.
type tableCopyPlan struct {
	Name          string
	Columns       []string
	ResetSequence bool
}

// orderedCopyPlan keeps foreign-key dependencies satisfied during the import.
var orderedCopyPlan = []tableCopyPlan{
	{Name: "users", Columns: []string{"id", "linuxdo_user_id", "username", "display_name", "avatar_url", "trust_level", "is_linuxdo_admin", "is_app_admin", "created_at", "updated_at", "last_login_at"}, ResetSequence: true},
	{Name: "oauth_states", Columns: []string{"id", "code_verifier", "next_path", "expires_at", "created_at"}},
	{Name: "managed_domains", Columns: []string{"id", "root_domain", "cloudflare_zone_id", "default_quota", "auto_provision", "is_default", "enabled", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "sessions", Columns: []string{"id", "user_id", "csrf_token", "user_agent_fingerprint", "expires_at", "created_at", "last_seen_at", "admin_verified_at"}},
	{Name: "user_controls", Columns: []string{"user_id", "is_banned", "note", "created_at", "updated_at"}},
	{Name: "user_domain_quotas", Columns: []string{"id", "user_id", "managed_domain_id", "max_allocations", "reason", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "allocations", Columns: []string{"id", "user_id", "managed_domain_id", "prefix", "normalized_prefix", "fqdn", "is_primary", "source", "status", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "email_routes", Columns: []string{"id", "owner_user_id", "root_domain", "prefix", "target_email", "enabled", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "admin_applications", Columns: []string{"id", "applicant_user_id", "type", "target", "reason", "status", "review_note", "reviewed_by_user_id", "reviewed_at", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "redeem_codes", Columns: []string{"id", "code", "type", "target", "note", "created_by_user_id", "used_by_user_id", "used_at", "created_at"}, ResetSequence: true},
	{Name: "permission_policies", Columns: []string{"key", "display_name", "description", "enabled", "auto_approve", "min_trust_level", "default_daily_limit", "created_at", "updated_at"}},
	{Name: "email_targets", Columns: []string{"id", "owner_user_id", "email", "cloudflare_address_id", "verified_at", "last_verification_sent_at", "created_at", "updated_at"}, ResetSequence: true},
	{Name: "quantity_records", Columns: []string{"id", "user_id", "resource_key", "scope", "delta", "source", "reason", "reference_type", "reference_id", "expires_at", "created_by_user_id", "created_at"}, ResetSequence: true},
	{Name: "email_catch_all_access", Columns: []string{"user_id", "subscription_expires_at", "remaining_count", "daily_limit_override", "created_at", "updated_at"}},
	{Name: "email_catch_all_daily_usage", Columns: []string{"user_id", "usage_date", "used_count", "created_at", "updated_at"}},
	{Name: "audit_logs", Columns: []string{"id", "actor_user_id", "action", "resource_type", "resource_id", "metadata_json", "created_at"}, ResetSequence: true},
}

func main() {
	sqlitePath := flag.String("sqlite-path", envOrDefault("SQLITE_PATH", "./data/linuxdospace.sqlite"), "Path to the source SQLite database file")
	postgresDSN := flag.String("postgres-dsn", firstNonEmptyEnv("DATABASE_POSTGRES_DSN", "DATABASE_URL"), "DSN for the target PostgreSQL database")
	truncateTarget := flag.Bool("truncate-target", true, "Whether to truncate PostgreSQL target tables before importing")
	flag.Parse()

	if strings.TrimSpace(*sqlitePath) == "" {
		log.Fatal("sqlite-path is required")
	}
	if strings.TrimSpace(*postgresDSN) == "" {
		log.Fatal("postgres-dsn is required")
	}

	ctx := context.Background()

	sqliteDB, err := sql.Open("sqlite", *sqlitePath)
	if err != nil {
		log.Fatalf("open sqlite database: %v", err)
	}
	defer sqliteDB.Close()

	postgresDB, err := sql.Open("pgx", *postgresDSN)
	if err != nil {
		log.Fatalf("open postgres database: %v", err)
	}
	defer postgresDB.Close()

	if err := postgresDB.PingContext(ctx); err != nil {
		log.Fatalf("ping postgres database: %v", err)
	}

	postgresStore, err := postgres.NewStore(*postgresDSN)
	if err != nil {
		log.Fatalf("open postgres store for schema bootstrap: %v", err)
	}
	if err := postgresStore.Migrate(ctx); err != nil {
		postgresStore.Close()
		log.Fatalf("migrate postgres schema before import: %v", err)
	}
	if err := postgresStore.Close(); err != nil {
		log.Fatalf("close bootstrap postgres store: %v", err)
	}

	tx, err := postgresDB.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("begin postgres transaction: %v", err)
	}
	defer tx.Rollback()

	if *truncateTarget {
		if err := truncateTargetTables(ctx, tx); err != nil {
			log.Fatalf("truncate postgres target tables: %v", err)
		}
	}

	for _, plan := range orderedCopyPlan {
		copiedRows, err := copyTable(ctx, sqliteDB, tx, plan)
		if err != nil {
			log.Fatalf("copy table %s: %v", plan.Name, err)
		}
		log.Printf("copied %d rows into %s", copiedRows, plan.Name)
	}

	for _, plan := range orderedCopyPlan {
		if !plan.ResetSequence {
			continue
		}
		if err := resetSequence(ctx, tx, plan.Name); err != nil {
			log.Fatalf("reset sequence for %s: %v", plan.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("commit postgres transaction: %v", err)
	}

	log.Printf("sqlite to postgres migration completed successfully")
}

// copyTable streams one full table from SQLite to PostgreSQL in the order
// defined by the migration plan.
func copyTable(ctx context.Context, sqliteDB *sql.DB, postgresTx *sql.Tx, plan tableCopyPlan) (int, error) {
	selectQuery := fmt.Sprintf(
		"SELECT %s FROM %s",
		strings.Join(plan.Columns, ", "),
		plan.Name,
	)
	if containsColumn(plan.Columns, "id") {
		selectQuery += " ORDER BY id"
	}

	rows, err := sqliteDB.QueryContext(ctx, selectQuery)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	insertQuery := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		plan.Name,
		strings.Join(plan.Columns, ", "),
		postgresPlaceholders(len(plan.Columns)),
	)

	copiedRows := 0
	for rows.Next() {
		values := make([]any, len(plan.Columns))
		destinations := make([]any, len(plan.Columns))
		for index := range values {
			destinations[index] = &values[index]
		}

		if err := rows.Scan(destinations...); err != nil {
			return copiedRows, err
		}

		for index, value := range values {
			values[index] = normalizeSQLiteValue(value)
		}

		if _, err := postgresTx.ExecContext(ctx, insertQuery, values...); err != nil {
			return copiedRows, err
		}
		copiedRows++
	}

	if err := rows.Err(); err != nil {
		return copiedRows, err
	}

	return copiedRows, nil
}

// truncateTargetTables clears all managed tables before one full import.
func truncateTargetTables(ctx context.Context, postgresTx *sql.Tx) error {
	tableNames := make([]string, 0, len(orderedCopyPlan))
	for _, plan := range orderedCopyPlan {
		tableNames = append(tableNames, plan.Name)
	}

	query := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(tableNames, ", "))
	_, err := postgresTx.ExecContext(ctx, query)
	return err
}

// resetSequence aligns every BIGSERIAL-backed sequence with the imported ids so
// future inserts continue from the correct next value.
func resetSequence(ctx context.Context, postgresTx *sql.Tx, tableName string) error {
	query := fmt.Sprintf(
		"SELECT setval(pg_get_serial_sequence('%s', 'id'), COALESCE((SELECT MAX(id) FROM %s), 1), true)",
		tableName,
		tableName,
	)
	_, err := postgresTx.ExecContext(ctx, query)
	return err
}

// postgresPlaceholders builds one `$1, $2, ...` placeholder list.
func postgresPlaceholders(count int) string {
	parts := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		parts = append(parts, fmt.Sprintf("$%d", index))
	}
	return strings.Join(parts, ", ")
}

// normalizeSQLiteValue converts SQLite driver return values into PostgreSQL-safe
// insert parameters. Text columns arrive as []byte and must become strings so
// PostgreSQL does not interpret them as bytea.
func normalizeSQLiteValue(value any) any {
	switch typedValue := value.(type) {
	case []byte:
		return string(typedValue)
	default:
		return typedValue
	}
}

// containsColumn checks whether the current copy plan contains one specific
// column name.
func containsColumn(columns []string, target string) bool {
	for _, column := range columns {
		if column == target {
			return true
		}
	}
	return false
}

// envOrDefault returns one trimmed environment value or a fallback string.
func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// firstNonEmptyEnv returns the first non-empty environment value among the
// provided keys.
func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
