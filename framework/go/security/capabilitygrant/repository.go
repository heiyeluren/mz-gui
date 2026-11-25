package capabilitygrant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLiteRepository struct {
	database *sql.DB
}

func NewSQLiteRepository(database *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{database: database}
}

func (repository *SQLiteRepository) Create(ctx context.Context, request CreateRequest) (Grant, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return Grant{}, ErrInvalidArgument
	}
	scopeJSON, err := validateCreateRequest(request)
	if err != nil {
		return Grant{}, err
	}
	grant := Grant{
		ID:         request.ID,
		Subject:    request.Subject,
		Capability: request.Capability,
		ScopeJSON:  scopeJSON,
		RiskLevel:  request.RiskLevel,
		GrantedAt:  request.Now.UTC(),
		ExpiresAt:  utcPointer(request.ExpiresAt),
		Source:     request.Source,
		Revision:   1,
	}
	_, err = repository.database.ExecContext(ctx, `INSERT INTO mzfw_capability_grants (
grant_id, subject, capability, scope_json, risk_level, granted_at, expires_at, revoked_at, grant_source, revision
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		grant.ID, grant.Subject, grant.Capability, grant.ScopeJSON, string(grant.RiskLevel), formatTime(grant.GrantedAt),
		nullableTime(grant.ExpiresAt), grant.Source, grant.Revision)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return Grant{}, ErrGrantAlreadyExists
		}
		return Grant{}, fmt.Errorf("写入 Capability Grant: %w", err)
	}
	return grant, nil
}

func (repository *SQLiteRepository) Get(ctx context.Context, grantID string) (Grant, error) {
	if repository == nil || repository.database == nil || ctx == nil || !identifierPattern.MatchString(grantID) {
		return Grant{}, ErrInvalidArgument
	}
	return loadGrant(ctx, repository.database, grantID)
}

func (repository *SQLiteRepository) Resolve(ctx context.Context, grantID string, now time.Time) (Grant, error) {
	if now.IsZero() {
		return Grant{}, ErrInvalidArgument
	}
	grant, err := repository.Get(ctx, grantID)
	if err != nil {
		return Grant{}, err
	}
	if !grant.Active(now) {
		return Grant{}, ErrGrantInactive
	}
	return grant, nil
}

func (repository *SQLiteRepository) List(ctx context.Context, query Query) ([]Grant, error) {
	if repository == nil || repository.database == nil || ctx == nil {
		return nil, ErrInvalidArgument
	}
	limit, err := validateQuery(query)
	if err != nil {
		return nil, err
	}
	where := []string{"1=1"}
	arguments := []any{}
	if query.Subject != "" {
		where = append(where, "subject = ?")
		arguments = append(arguments, query.Subject)
	}
	if query.Capability != "" {
		where = append(where, "capability = ?")
		arguments = append(arguments, query.Capability)
	}
	if query.Source != "" {
		where = append(where, "grant_source = ?")
		arguments = append(arguments, query.Source)
	}
	if !query.IncludeInactive {
		where = append(where, "revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)")
		arguments = append(arguments, formatTime(query.Now.UTC()))
	}
	arguments = append(arguments, limit)
	rows, err := repository.database.QueryContext(ctx, `SELECT grant_id, subject, capability, scope_json, risk_level, granted_at,
expires_at, revoked_at, grant_source, revision FROM mzfw_capability_grants WHERE `+strings.Join(where, " AND ")+
		` ORDER BY granted_at DESC, grant_id DESC LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("查询 Capability Grants: %w", err)
	}
	defer rows.Close()
	grants := make([]Grant, 0)
	for rows.Next() {
		grant, scanErr := scanGrant(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取 Capability Grants: %w", err)
	}
	return grants, nil
}

func (repository *SQLiteRepository) Revoke(ctx context.Context, grantID string, expectedRevision int64, now time.Time) (Grant, error) {
	if repository == nil || repository.database == nil || ctx == nil || !identifierPattern.MatchString(grantID) ||
		expectedRevision < 1 || now.IsZero() {
		return Grant{}, ErrInvalidArgument
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Grant{}, fmt.Errorf("开始撤销 Capability Grant 事务: %w", err)
	}
	defer transaction.Rollback()
	grant, err := loadGrant(ctx, transaction, grantID)
	if err != nil {
		return Grant{}, err
	}
	if grant.Revision != expectedRevision {
		return Grant{}, ErrRevisionConflict
	}
	if grant.RevokedAt != nil {
		return Grant{}, ErrGrantInactive
	}
	revokedAt := now.UTC()
	grant.RevokedAt = &revokedAt
	grant.Revision++
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_capability_grants SET revoked_at = ?, revision = ?
WHERE grant_id = ? AND revision = ?`, formatTime(revokedAt), grant.Revision, grant.ID, expectedRevision)
	if err != nil {
		return Grant{}, fmt.Errorf("撤销 Capability Grant: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return Grant{}, ErrRevisionConflict
	}
	if err := transaction.Commit(); err != nil {
		return Grant{}, fmt.Errorf("提交撤销 Capability Grant: %w", err)
	}
	return grant, nil
}

type rowScanner interface{ Scan(...any) error }

func loadGrant(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, grantID string) (Grant, error) {
	row := queryer.QueryRowContext(ctx, `SELECT grant_id, subject, capability, scope_json, risk_level, granted_at,
expires_at, revoked_at, grant_source, revision FROM mzfw_capability_grants WHERE grant_id = ?`, grantID)
	grant, err := scanGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrGrantNotFound
	}
	return grant, err
}

func scanGrant(row rowScanner) (Grant, error) {
	var grant Grant
	var riskLevel string
	var grantedAt string
	var expiresAt, revokedAt sql.NullString
	if err := row.Scan(&grant.ID, &grant.Subject, &grant.Capability, &grant.ScopeJSON, &riskLevel, &grantedAt,
		&expiresAt, &revokedAt, &grant.Source, &grant.Revision); err != nil {
		return Grant{}, err
	}
	grant.RiskLevel = RiskLevel(riskLevel)
	if !validRiskLevel(grant.RiskLevel) {
		return Grant{}, ErrInvalidArgument
	}
	if _, err := normalizeScopeJSON([]byte(grant.ScopeJSON)); err != nil {
		return Grant{}, err
	}
	parsedGrantedAt, err := parseTime(grantedAt)
	if err != nil {
		return Grant{}, err
	}
	grant.GrantedAt = parsedGrantedAt
	grant.ExpiresAt, err = parseOptionalTime(expiresAt)
	if err != nil {
		return Grant{}, err
	}
	grant.RevokedAt, err = parseOptionalTime(revokedAt)
	return grant, err
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	result := value.UTC()
	return &result
}
