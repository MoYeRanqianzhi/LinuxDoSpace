package sqlite

import (
	"bytes"
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

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

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

	// 线上迁移时验证过，modernc sqlite 在某些 bind mount 场景下如果数据库文件尚未存在，
	// 首次打开会直接返回 “unable to open database file”。
	// 这里先显式创建空文件，把“首次建库”从驱动层前移到标准库文件操作，避免新服务器首启失败。
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o664)
	if err != nil {
		return nil, fmt.Errorf("create sqlite database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close sqlite database file after create: %w", err)
	}

	// SQLite 只用于本地开发、测试和回滚兜底路径，但这里仍然需要把并发写的
	// 行为收紧到“默认就安全”的状态。modernc.org/sqlite 支持通过 `_txlock`
	// 驱动参数把事务开始模式切到 `immediate`，从而避免多个连接都先拿到
	// 延迟事务快照、随后在升级成写事务时互相打出 `SQLITE_BUSY`。
	//
	// 同时把 `busy_timeout` 与 `journal_mode=WAL` 固化到连接串里：
	//   1. `busy_timeout` 让 SQLite 在短时写锁竞争下先等待，而不是立刻失败；
	//   2. `WAL` 让读写并发行为更接近我们在测试里验证的场景。
	//
	// 这里使用驱动级默认值而不是在每个 repository 方法内手写
	// `BEGIN IMMEDIATE`，因为后者非常容易漏掉未来新增的写事务入口。
	dsn := path + "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
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
		script = bytes.TrimPrefix(script, utf8BOM)

		for _, statement := range splitMigrationStatements(string(script)) {
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				if isIgnorableMigrationError(err) {
					continue
				}
				return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// splitMigrationStatements executes SQLite migrations statement-by-statement so
// one ignorable duplicate-column error does not skip the rest of the file.
// The embedded migration files are controlled project SQL and intentionally do
// not contain semicolons inside string literals.
func splitMigrationStatements(script string) []string {
	rawStatements := strings.Split(script, ";")
	statements := make([]string, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		trimmed := strings.TrimSpace(rawStatement)
		if trimmed == "" {
			continue
		}
		statements = append(statements, trimmed)
	}
	return statements
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
