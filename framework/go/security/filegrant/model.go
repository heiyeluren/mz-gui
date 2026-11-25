// Package filegrant 提供产品无关的本机文件授权模型与校验机制。
package filegrant

import (
	"encoding/json"
	"errors"
	"time"
)

type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
)

type Access string

const (
	AccessRead      Access = "read"
	AccessReadWrite Access = "read_write"
)

var (
	ErrInvalidArgument  = errors.New("文件授权请求无效")
	ErrPermissionDenied = errors.New("系统未授权访问该目录")
	ErrSymbolicLink     = errors.New("文件授权不允许符号链接")
	ErrKindMismatch     = errors.New("文件授权路径类型不匹配")
	ErrInvalidScope     = errors.New("文件授权 scope 无效")
)

type Grant struct {
	ID         string
	Subject    string
	Capability string
	Path       string
	Kind       Kind
	Access     Access
	RiskLevel  string
	GrantedAt  time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	Source     string
	Revision   int64
}

func (grant Grant) Active(now time.Time) bool {
	return grant.RevokedAt == nil && (grant.ExpiresAt == nil || grant.ExpiresAt.After(now.UTC()))
}

type CreateRequest struct {
	Subject   string
	Path      string
	Kind      Kind
	Access    Access
	Source    string
	ExpiresAt *time.Time
	Now       time.Time
}

type Scope struct {
	SchemaVersion int    `json:"schema_version"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Kind          Kind   `json:"kind"`
	Access        Access `json:"access"`
}

func Capability(kind Kind, access Access) string {
	return "filesystem." + string(kind) + "." + string(access)
}

func EncodeScope(scope Scope) ([]byte, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		return nil, ErrInvalidScope
	}
	return encoded, nil
}

func DecodeScope(encoded []byte) (Scope, error) {
	var scope Scope
	if err := json.Unmarshal(encoded, &scope); err != nil {
		return Scope{}, ErrInvalidScope
	}
	if err := validateScope(scope); err != nil {
		return Scope{}, err
	}
	return scope, nil
}

func validateScope(scope Scope) error {
	if scope.SchemaVersion != 1 || scope.Type != "filesystem" || scope.Path == "" {
		return ErrInvalidScope
	}
	if scope.Kind != KindFile && scope.Kind != KindDirectory {
		return ErrInvalidScope
	}
	if scope.Access != AccessRead && scope.Access != AccessReadWrite {
		return ErrInvalidScope
	}
	return nil
}
