package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type SchemaObjectType string

const (
	SchemaObjectTable   SchemaObjectType = "table"
	SchemaObjectIndex   SchemaObjectType = "index"
	SchemaObjectTrigger SchemaObjectType = "trigger"
	SchemaObjectView    SchemaObjectType = "view"
)

type SchemaObject struct {
	Type SchemaObjectType
	Name string
}

type MigrationBundle struct {
	Owner        string
	Dependencies []string
	Migrations   fs.FS
	MigrationDir string
	Objects      []SchemaObject
	AdoptLegacy  bool
}

var (
	bundleOwnerPattern = regexp.MustCompile(`^(framework|adapter|application)\.[a-z0-9][a-z0-9.-]*$`)
	objectNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
)

func migrateBundles(ctx context.Context, database *sql.DB, options Options) error {
	ordered, err := planBundles(options.Bundles)
	if err != nil {
		return err
	}
	if err := createBundleRegistry(ctx, database); err != nil {
		return err
	}
	for _, bundle := range ordered {
		if err := applyBundle(ctx, database, options, bundle); err != nil {
			return err
		}
	}
	return nil
}

func createBundleRegistry(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mzfw_schema_migrations (
owner TEXT NOT NULL,
version INTEGER NOT NULL,
name TEXT NOT NULL,
checksum TEXT NOT NULL,
applied_at TEXT NOT NULL,
adopted_from TEXT,
PRIMARY KEY(owner, version)
);
CREATE TABLE IF NOT EXISTS mzfw_schema_bundles (
owner TEXT PRIMARY KEY,
dependencies_json TEXT NOT NULL,
updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS mzfw_schema_objects (
object_type TEXT NOT NULL,
object_name TEXT NOT NULL,
owner TEXT NOT NULL,
PRIMARY KEY(object_type, object_name)
);`)
	if err != nil {
		return fmt.Errorf("创建 Framework migration registry: %w", err)
	}
	return nil
}

func planBundles(input []MigrationBundle) ([]MigrationBundle, error) {
	bundles := make(map[string]MigrationBundle, len(input))
	objectOwners := make(map[string]string)
	for _, source := range input {
		bundle := source
		bundle.Owner = strings.TrimSpace(bundle.Owner)
		if !bundleOwnerPattern.MatchString(bundle.Owner) || bundle.Migrations == nil {
			return nil, fmt.Errorf("SQLite migration bundle 无效: owner=%q", bundle.Owner)
		}
		if bundle.MigrationDir == "" {
			bundle.MigrationDir = "."
		}
		if _, exists := bundles[bundle.Owner]; exists {
			return nil, fmt.Errorf("SQLite migration bundle owner 重复: %s", bundle.Owner)
		}
		dependencies := make(map[string]struct{}, len(bundle.Dependencies))
		for _, dependency := range bundle.Dependencies {
			if !bundleOwnerPattern.MatchString(dependency) || dependency == bundle.Owner {
				return nil, fmt.Errorf("SQLite migration bundle %s 依赖无效: %s", bundle.Owner, dependency)
			}
			if _, exists := dependencies[dependency]; exists {
				return nil, fmt.Errorf("SQLite migration bundle %s 依赖重复: %s", bundle.Owner, dependency)
			}
			dependencies[dependency] = struct{}{}
		}
		for _, object := range bundle.Objects {
			if !object.Type.valid() || !objectNamePattern.MatchString(object.Name) {
				return nil, fmt.Errorf("SQLite migration bundle %s 对象无效: %s/%s", bundle.Owner, object.Type, object.Name)
			}
			if strings.HasPrefix(bundle.Owner, "framework.") && !strings.HasPrefix(object.Name, "mzfw_") {
				return nil, fmt.Errorf("Framework 数据对象必须使用 mzfw_ 前缀: %s", object.Name)
			}
			if strings.HasPrefix(bundle.Owner, "application.") && strings.HasPrefix(object.Name, "mzfw_") {
				return nil, fmt.Errorf("Application 不得声明 Framework 数据对象: %s", object.Name)
			}
			key := string(object.Type) + ":" + object.Name
			if owner, exists := objectOwners[key]; exists {
				return nil, fmt.Errorf("SQLite 对象 %s 被 %s 和 %s 重复声明", object.Name, owner, bundle.Owner)
			}
			objectOwners[key] = bundle.Owner
		}
		bundle.Dependencies = sortedKeys(dependencies)
		bundles[bundle.Owner] = bundle
	}

	indegree := make(map[string]int, len(bundles))
	children := make(map[string][]string, len(bundles))
	for owner, bundle := range bundles {
		for _, dependency := range bundle.Dependencies {
			if _, exists := bundles[dependency]; !exists {
				return nil, fmt.Errorf("SQLite migration bundle %s 缺少依赖: %s", owner, dependency)
			}
			indegree[owner]++
			children[dependency] = append(children[dependency], owner)
		}
	}
	ready := make([]string, 0, len(bundles))
	for owner := range bundles {
		if indegree[owner] == 0 {
			ready = append(ready, owner)
		}
	}
	sortBundleOwners(ready)
	ordered := make([]MigrationBundle, 0, len(bundles))
	for len(ready) > 0 {
		owner := ready[0]
		ready = ready[1:]
		ordered = append(ordered, bundles[owner])
		for _, child := range children[owner] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
			}
		}
		sortBundleOwners(ready)
	}
	if len(ordered) != len(bundles) {
		return nil, fmt.Errorf("SQLite migration bundle 存在循环依赖")
	}
	return ordered, nil
}

func applyBundle(ctx context.Context, database *sql.DB, options Options, bundle MigrationBundle) error {
	entries, err := fs.ReadDir(bundle.Migrations, bundle.MigrationDir)
	if err != nil {
		return fmt.Errorf("读取 migration bundle %s: %w", bundle.Owner, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	versions := make(map[int]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		if previous, exists := versions[version]; exists {
			return fmt.Errorf("migration bundle %s 版本 %d 重复: %s/%s", bundle.Owner, version, previous, entry.Name())
		}
		versions[version] = entry.Name()
		if err := applyBundleMigration(ctx, database, options, bundle, version, entry.Name()); err != nil {
			return err
		}
	}
	if err := registerBundle(ctx, database, options, bundle); err != nil {
		return err
	}
	return nil
}

func applyBundleMigration(ctx context.Context, database *sql.DB, options Options, bundle MigrationBundle, version int, name string) error {
	data, err := fs.ReadFile(bundle.Migrations, path.Join(bundle.MigrationDir, name))
	if err != nil {
		return fmt.Errorf("读取 migration bundle %s/%s: %w", bundle.Owner, name, err)
	}
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	var existing string
	err = database.QueryRowContext(ctx,
		"SELECT checksum FROM mzfw_schema_migrations WHERE owner = ? AND version = ?", bundle.Owner, version).Scan(&existing)
	if err == nil {
		if existing != checksum {
			return fmt.Errorf("migration bundle %s 版本 %d checksum 不一致", bundle.Owner, version)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("检查 migration bundle %s/%d: %w", bundle.Owner, version, err)
	}
	if bundle.AdoptLegacy {
		adopted, err := adoptLegacyMigration(ctx, database, options, bundle.Owner, version, name, checksum)
		if err != nil {
			return err
		}
		if adopted {
			return nil
		}
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始 migration bundle %s/%d: %w", bundle.Owner, version, err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("执行 migration bundle %s/%d: %w", bundle.Owner, version, err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_schema_migrations
(owner, version, name, checksum, applied_at) VALUES (?, ?, ?, ?, ?)`,
		bundle.Owner, version, name, checksum, options.Now().UTC().Format(timeFormat)); err != nil {
		return fmt.Errorf("记录 migration bundle %s/%d: %w", bundle.Owner, version, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交 migration bundle %s/%d: %w", bundle.Owner, version, err)
	}
	return nil
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func adoptLegacyMigration(ctx context.Context, database *sql.DB, options Options, owner string, version int, name, checksum string) (bool, error) {
	var legacyName, legacyChecksum, appliedAt string
	err := database.QueryRowContext(ctx,
		"SELECT name, checksum, applied_at FROM schema_migrations WHERE version = ?", version).
		Scan(&legacyName, &legacyChecksum, &appliedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取旧迁移记录 %d: %w", version, err)
	}
	if legacyName != name || legacyChecksum != checksum {
		return false, fmt.Errorf("旧迁移 %d 与 migration bundle %s 不一致", version, owner)
	}
	if appliedAt == "" {
		appliedAt = options.Now().UTC().Format(timeFormat)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO mzfw_schema_migrations
(owner, version, name, checksum, applied_at, adopted_from) VALUES (?, ?, ?, ?, ?, ?)`,
		owner, version, name, checksum, appliedAt, "schema_migrations")
	if err != nil {
		return false, fmt.Errorf("接管旧迁移 %s/%d: %w", owner, version, err)
	}
	return true, nil
}

func registerBundle(ctx context.Context, database *sql.DB, options Options, bundle MigrationBundle) error {
	dependencies, err := json.Marshal(bundle.Dependencies)
	if err != nil {
		return fmt.Errorf("编码 migration bundle 依赖: %w", err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("登记 migration bundle %s: %w", bundle.Owner, err)
	}
	defer transaction.Rollback()
	for _, object := range bundle.Objects {
		var count int
		if err := transaction.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", object.Type, object.Name).Scan(&count); err != nil {
			return fmt.Errorf("检查 SQLite 对象 %s: %w", object.Name, err)
		}
		if count != 1 {
			return fmt.Errorf("migration bundle %s 未创建声明对象 %s/%s", bundle.Owner, object.Type, object.Name)
		}
		var owner string
		err := transaction.QueryRowContext(ctx,
			"SELECT owner FROM mzfw_schema_objects WHERE object_type = ? AND object_name = ?", object.Type, object.Name).Scan(&owner)
		switch {
		case err == nil && owner != bundle.Owner:
			return fmt.Errorf("SQLite 对象 %s 已属于 %s", object.Name, owner)
		case err == sql.ErrNoRows:
			if _, err := transaction.ExecContext(ctx,
				"INSERT INTO mzfw_schema_objects(object_type, object_name, owner) VALUES (?, ?, ?)", object.Type, object.Name, bundle.Owner); err != nil {
				return fmt.Errorf("登记 SQLite 对象 %s: %w", object.Name, err)
			}
		case err != nil:
			return fmt.Errorf("读取 SQLite 对象 owner %s: %w", object.Name, err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_schema_bundles(owner, dependencies_json, updated_at)
VALUES (?, ?, ?) ON CONFLICT(owner) DO UPDATE SET dependencies_json=excluded.dependencies_json, updated_at=excluded.updated_at`,
		bundle.Owner, string(dependencies), options.Now().UTC().Format(timeFormat)); err != nil {
		return fmt.Errorf("登记 migration bundle %s: %w", bundle.Owner, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("提交 migration bundle registry %s: %w", bundle.Owner, err)
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	versionText, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("迁移文件名无版本: %s", name)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("迁移版本无效: %s", name)
	}
	return version, nil
}

func (objectType SchemaObjectType) valid() bool {
	return objectType == SchemaObjectTable || objectType == SchemaObjectIndex ||
		objectType == SchemaObjectTrigger || objectType == SchemaObjectView
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortBundleOwners(owners []string) {
	sort.Slice(owners, func(i, j int) bool {
		leftRank, rightRank := bundleOwnerRank(owners[i]), bundleOwnerRank(owners[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return owners[i] < owners[j]
	})
}

func bundleOwnerRank(owner string) int {
	switch {
	case strings.HasPrefix(owner, "framework."):
		return 0
	case strings.HasPrefix(owner, "adapter."):
		return 1
	default:
		return 2
	}
}
