package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// migrationsFS 用来嵌入 SQL 迁移文件，以便二进制程序可以自包含地完成数据库初始化。
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store 是 SQLite 持久化层的入口对象。
// 当前阶段先提供数据库生命周期与迁移能力，业务读写方法会在下一阶段接入。
type Store struct {
	db *sql.DB
}

// NewStore 打开一个 SQLite 数据库连接，并确保目标目录存在。
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)

	return &Store{db: db}, nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB 暴露底层数据库连接。
// 这个方法主要给需要直接编写查询的上层逻辑使用。
func (s *Store) DB() *sql.DB {
	return s.db
}

// Migrate 执行所有嵌入的 SQL 迁移文件。
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migration directory: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		script, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		if _, err := s.db.ExecContext(ctx, string(script)); err != nil {
			if isIgnorableMigrationError(err) {
				continue
			}
			return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// isIgnorableMigrationError reports whether one migration failure only means the
// schema change was already applied on a previous run.
func isIgnorableMigrationError(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "duplicate column name:")
}
