package filegrant

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Policy 由上层应用注入主体、来源、权限和有效期规则。
type Policy struct {
	AllowedSubjects []string
	AllowedSources  []string
	MaxPathLength   int
	MaxLifetime     time.Duration
	PersistentKinds []Kind
	AccessByKind    map[Kind][]Access
}

func (policy Policy) ValidateCreate(request CreateRequest, idempotencyKey string) (string, error) {
	if request.Subject == "" || request.Source == "" || request.Now.IsZero() || idempotencyKey == "" || request.Path == "" {
		return "", ErrInvalidArgument
	}
	if policy.MaxPathLength > 0 && len(request.Path) > policy.MaxPathLength {
		return "", ErrInvalidArgument
	}
	if !allowedString(policy.AllowedSubjects, request.Subject) || !allowedString(policy.AllowedSources, request.Source) {
		return "", ErrInvalidArgument
	}
	if !allowedAccess(policy.AccessByKind[request.Kind], request.Access) {
		return "", ErrInvalidArgument
	}
	if request.ExpiresAt == nil {
		if !allowedKind(policy.PersistentKinds, request.Kind) {
			return "", ErrInvalidArgument
		}
	} else if !request.ExpiresAt.After(request.Now) || (policy.MaxLifetime > 0 && request.ExpiresAt.After(request.Now.Add(policy.MaxLifetime))) {
		return "", ErrInvalidArgument
	}
	return ValidatePath(request.Path, request.Kind)
}

func ValidatePath(path string, kind Kind) (string, error) {
	if path == "" || !filepath.IsAbs(path) || (kind != KindFile && kind != KindDirectory) {
		return "", ErrInvalidArgument
	}
	cleaned := filepath.Clean(path)
	info, err := os.Lstat(cleaned)
	if err != nil {
		return "", MapPathError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrSymbolicLink
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", MapPathError(err)
	}
	info, err = os.Stat(resolved)
	if err != nil {
		return "", MapPathError(err)
	}
	if (kind == KindDirectory) != info.IsDir() {
		return "", ErrKindMismatch
	}
	return filepath.Abs(resolved)
}

func MapPathError(err error) error {
	if err == nil {
		return nil
	}
	if IsPermissionDenied(err) {
		return ErrPermissionDenied
	}
	return ErrInvalidArgument
}

func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPermissionDenied) || errors.Is(err, os.ErrPermission) || os.IsPermission(err) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "permission denied") ||
		strings.Contains(text, "operation not permitted") ||
		strings.Contains(text, "eacces") ||
		strings.Contains(text, "eperm") ||
		strings.Contains(text, "tcc")
}

func allowedString(values []string, candidate string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func allowedAccess(values []Access, candidate Access) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func allowedKind(values []Kind, candidate Kind) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
