package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Options struct {
	Path            string
	Migrations      fs.FS
	Bundles         []MigrationBundle
	MigrationDir    string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	BusyTimeout     time.Duration
	Now             func() time.Time
}

// Open 打开 SQLite 数据库并执行调用方提供的只向前迁移。
func Open(ctx context.Context, options Options) (*sql.DB, error) {
	if ctx == nil {
		return nil, fmt.Errorf("SQLite context 不能为空")
	}
	if err := normalizeOptions(&options); err != nil {
		return nil, err
	}
	ctx, _, err := startSQLiteSpan(ctx, "sqlite.open")
	if err != nil {
		return nil, err
	}
	// modernc SQLite 的 busy handler 不响应 Go context，单次等待必须足够短，避免 GUI/RPC 假死。
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate", filepath.ToSlash(options.Path), options.BusyTimeout.Milliseconds())
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	database.SetMaxOpenConns(options.MaxOpenConns)
	database.SetMaxIdleConns(options.MaxIdleConns)
	database.SetConnMaxLifetime(options.ConnMaxLifetime)

	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("连接 SQLite: %w", err)
	}
	if err := migrate(ctx, database, options); err != nil {
		database.Close()
		return nil, err
	}
	if err := secureDatabaseFiles(options.Path); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func normalizeOptions(options *Options) error {
	if options.Path == "" {
		return fmt.Errorf("SQLite 路径不能为空")
	}
	if (options.Migrations == nil) == (len(options.Bundles) == 0) {
		return fmt.Errorf("SQLite 必须且只能配置旧迁移集合或 migration bundles")
	}
	if options.MigrationDir == "" {
		options.MigrationDir = "."
	}
	if options.MaxOpenConns == 0 {
		options.MaxOpenConns = 4
	}
	if options.MaxIdleConns == 0 {
		// 默认保留常用连接，避免并发查询后反复建连并重新解析完整数据库结构。
		options.MaxIdleConns = min(options.MaxOpenConns, 4)
	}
	if options.ConnMaxLifetime == 0 {
		options.ConnMaxLifetime = 30 * time.Minute
	}
	if options.BusyTimeout == 0 {
		options.BusyTimeout = 250 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxOpenConns < 1 || options.MaxOpenConns > 64 || options.MaxIdleConns < 0 ||
		options.MaxIdleConns > options.MaxOpenConns || options.ConnMaxLifetime < time.Minute ||
		options.BusyTimeout < 0 || options.BusyTimeout > 30*time.Second {
		return fmt.Errorf("SQLite 连接池配置无效")
	}
	return nil
}

func secureDatabaseFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("设置 SQLite 文件权限 %s: %w", filepath.Base(candidate), err)
		}
	}
	return nil
}

func migrate(ctx context.Context, database *sql.DB, options Options) error {
	ctx, _, err := startSQLiteSpan(ctx, "sqlite.migrate")
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version INTEGER PRIMARY KEY,
name TEXT NOT NULL,
checksum TEXT NOT NULL,
applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("创建迁移表: %w", err)
	}
	if len(options.Bundles) > 0 {
		return migrateBundles(ctx, database, options)
	}

	entries, err := fs.ReadDir(options.Migrations, options.MigrationDir)
	if err != nil {
		return fmt.Errorf("读取迁移集合: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := applyMigration(ctx, database, options, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, database *sql.DB, options Options, name string) error {
	versionText, _, ok := strings.Cut(name, "_")
	if !ok {
		return fmt.Errorf("迁移文件名无版本: %s", name)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil {
		return fmt.Errorf("迁移版本无效 %s: %w", name, err)
	}
	data, err := fs.ReadFile(options.Migrations, path.Join(options.MigrationDir, name))
	if err != nil {
		return fmt.Errorf("读取迁移 %s: %w", name, err)
	}
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(data))

	var existing string
	err = database.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&existing)
	if err == nil {
		if existing != checksum {
			return fmt.Errorf("迁移 %d checksum 不一致", version)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("检查迁移 %d: %w", version, err)
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始迁移 %d: %w", version, err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("执行迁移 %d: %w", version, err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
		version, name, checksum, options.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("记录迁移 %d: %w", version, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交迁移 %d: %w", version, err)
	}
	return nil
}
