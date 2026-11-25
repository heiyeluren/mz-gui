package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type LegacyTableMapping struct {
	Source  string
	Target  string
	Columns []string
}

// ReconcileLegacyTables 将完整旧表集合幂等复制到新表，并在同一事务逐行校验。
func ReconcileLegacyTables(ctx context.Context, database *sql.DB, mappings []LegacyTableMapping) error {
	if ctx == nil || database == nil || len(mappings) == 0 {
		return fmt.Errorf("SQLite legacy reconcile 参数无效")
	}
	for _, mapping := range mappings {
		if err := validateLegacyMapping(mapping); err != nil {
			return err
		}
	}
	present := 0
	for _, mapping := range mappings {
		var count int
		if err := database.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", mapping.Source).Scan(&count); err != nil {
			return fmt.Errorf("检查旧技术表 %s: %w", mapping.Source, err)
		}
		present += count
	}
	if present == 0 {
		return nil
	}
	if present != len(mappings) {
		return fmt.Errorf("旧技术 schema 不完整: found=%d want=%d", present, len(mappings))
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始旧技术表迁移: %w", err)
	}
	defer transaction.Rollback()
	for _, mapping := range mappings {
		if err := reconcileLegacyTable(ctx, transaction, mapping); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交旧技术表迁移: %w", err)
	}
	return nil
}

func validateLegacyMapping(mapping LegacyTableMapping) error {
	if !objectNamePattern.MatchString(mapping.Source) || !objectNamePattern.MatchString(mapping.Target) || len(mapping.Columns) == 0 {
		return fmt.Errorf("SQLite legacy table mapping 无效")
	}
	seen := make(map[string]struct{}, len(mapping.Columns))
	for _, column := range mapping.Columns {
		if !objectNamePattern.MatchString(column) {
			return fmt.Errorf("SQLite legacy column 无效: %s", column)
		}
		if _, exists := seen[column]; exists {
			return fmt.Errorf("SQLite legacy column 重复: %s", column)
		}
		seen[column] = struct{}{}
	}
	return nil
}

func reconcileLegacyTable(ctx context.Context, transaction *sql.Tx, mapping LegacyTableMapping) error {
	columns := strings.Join(mapping.Columns, ", ")
	copyStatement := fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s",
		mapping.Target, columns, columns, mapping.Source)
	if _, err := transaction.ExecContext(ctx, copyStatement); err != nil {
		return fmt.Errorf("复制旧技术表 %s: %w", mapping.Source, err)
	}
	verifyStatement := fmt.Sprintf("SELECT COUNT(*) FROM (SELECT %s FROM %s EXCEPT SELECT %s FROM %s)",
		columns, mapping.Source, columns, mapping.Target)
	var missing int
	if err := transaction.QueryRowContext(ctx, verifyStatement).Scan(&missing); err != nil {
		return fmt.Errorf("校验旧技术表 %s: %w", mapping.Source, err)
	}
	if missing != 0 {
		return fmt.Errorf("旧技术表 %s 数据与 %s 冲突: rows=%d", mapping.Source, mapping.Target, missing)
	}
	return nil
}
