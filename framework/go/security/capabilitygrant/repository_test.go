package capabilitygrant

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	capabilitymigrations "github.com/heiyeluren/mz-gui/framework/go/security/capabilitygrant/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestSQLiteRepositoryCreatesResolvesListsAndRevokesGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, database := openTestRepository(t)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)

	created, err := repository.Create(ctx, CreateRequest{
		ID:         "grant-test-1",
		Subject:    "process:desktop",
		Capability: "filesystem.file.read",
		ScopeJSON:  []byte(`{"schema_version":1,"type":"filesystem","path_ref":"file-a"}`),
		RiskLevel:  RiskMedium,
		Source:     "native_dialog",
		Now:        now,
		ExpiresAt:  &expiresAt,
	})
	if err != nil {
		t.Fatalf("创建 Framework Grant 失败: %v", err)
	}
	if created.Revision != 1 || !created.Active(now) || created.ScopeJSON == "" {
		t.Fatalf("创建结果异常: %#v", created)
	}
	if _, err := repository.Create(ctx, CreateRequest{
		ID:         "grant-test-1",
		Subject:    "process:desktop",
		Capability: "filesystem.file.read",
		ScopeJSON:  []byte(`{"schema_version":1}`),
		RiskLevel:  RiskMedium,
		Source:     "native_dialog",
		Now:        now,
	}); !errors.Is(err, ErrGrantAlreadyExists) {
		t.Fatalf("重复 grant_id 必须拒绝: %v", err)
	}

	resolved, err := repository.Resolve(ctx, "grant-test-1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("解析活跃 Grant 失败: %v", err)
	}
	if resolved.ID != created.ID || resolved.ScopeJSON != created.ScopeJSON {
		t.Fatalf("解析结果不一致: %#v", resolved)
	}
	listed, err := repository.List(ctx, Query{
		Subject:    "process:desktop",
		Capability: "filesystem.file.read",
		Source:     "native_dialog",
		Now:        now,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("查询活跃 Grant 失败: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("查询结果异常: %#v", listed)
	}

	revoked, err := repository.Revoke(ctx, created.ID, created.Revision, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("撤销 Grant 失败: %v", err)
	}
	if revoked.RevokedAt == nil || revoked.Revision != 2 {
		t.Fatalf("撤销结果异常: %#v", revoked)
	}
	if _, err := repository.Resolve(ctx, created.ID, now.Add(3*time.Minute)); !errors.Is(err, ErrGrantInactive) {
		t.Fatalf("撤销后解析必须失效: %v", err)
	}
	inactive, err := repository.List(ctx, Query{Subject: "process:desktop", IncludeInactive: true, Limit: 10})
	if err != nil {
		t.Fatalf("查询非活跃 Grant 失败: %v", err)
	}
	if len(inactive) != 1 || inactive[0].Revision != 2 {
		t.Fatalf("非活跃查询结果异常: %#v", inactive)
	}

	var storedRevokedAt sql.NullString
	if err := database.QueryRow("SELECT revoked_at FROM mzfw_capability_grants WHERE grant_id = ?", created.ID).Scan(&storedRevokedAt); err != nil {
		t.Fatalf("回读 revoked_at 失败: %v", err)
	}
	if !storedRevokedAt.Valid {
		t.Fatal("撤销时间必须落库")
	}
}

func TestSQLiteRepositoryRejectsExpiredAndRevisionConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := openTestRepository(t)
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	created, err := repository.Create(ctx, CreateRequest{
		ID:         "grant-expiring",
		Subject:    "process:browser-host",
		Capability: "browser.dom.read",
		ScopeJSON:  []byte(`{"schema_version":1,"type":"browser","origin":"https://example.com"}`),
		RiskLevel:  RiskHigh,
		Source:     "browser_bridge",
		Now:        now,
		ExpiresAt:  &expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve(ctx, created.ID, expiresAt); !errors.Is(err, ErrGrantInactive) {
		t.Fatalf("到期边界必须失效: %v", err)
	}
	if _, err := repository.Revoke(ctx, created.ID, created.Revision+1, now.Add(30*time.Second)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("错误 revision 必须拒绝: %v", err)
	}
}

func TestSQLiteRepositoryValidatesArgumentsAndLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := openTestRepository(t)
	now := time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)
	invalidRequests := []CreateRequest{
		{ID: "bad id", Subject: "process:desktop", Capability: "filesystem.read", ScopeJSON: []byte(`{}`), RiskLevel: RiskMedium, Source: "native", Now: now},
		{ID: "grant-bad-subject", Subject: "bad subject", Capability: "filesystem.read", ScopeJSON: []byte(`{}`), RiskLevel: RiskMedium, Source: "native", Now: now},
		{ID: "grant-bad-capability", Subject: "process:desktop", Capability: "bad capability", ScopeJSON: []byte(`{}`), RiskLevel: RiskMedium, Source: "native", Now: now},
		{ID: "grant-bad-scope", Subject: "process:desktop", Capability: "filesystem.read", ScopeJSON: []byte(`[]`), RiskLevel: RiskMedium, Source: "native", Now: now},
		{ID: "grant-bad-risk", Subject: "process:desktop", Capability: "filesystem.read", ScopeJSON: []byte(`{}`), RiskLevel: "urgent", Source: "native", Now: now},
	}
	for _, request := range invalidRequests {
		if _, err := repository.Create(ctx, request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法创建参数必须拒绝 request=%#v err=%v", request, err)
		}
	}
	if _, err := repository.Resolve(ctx, "", now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空 Grant ID 必须拒绝: %v", err)
	}
	if _, err := repository.Resolve(ctx, "grant-missing", now); !errors.Is(err, ErrGrantNotFound) {
		t.Fatalf("不存在 Grant 必须明确返回 not found: %v", err)
	}
	if _, err := repository.List(ctx, Query{Subject: "process:desktop", Now: now, Limit: 501}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("超过上限的列表查询必须拒绝: %v", err)
	}
	if _, err := repository.List(ctx, Query{Subject: "bad subject", Now: now, Limit: 10}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法查询主体必须拒绝: %v", err)
	}
}

func openTestRepository(t *testing.T) (*SQLiteRepository, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	database, err := frameworksqlite.Open(ctx, frameworksqlite.Options{
		Path:    filepath.Join(t.TempDir(), "capability-grants.db"),
		Bundles: []frameworksqlite.MigrationBundle{capabilitymigrations.Bundle()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewSQLiteRepository(database), database
}
