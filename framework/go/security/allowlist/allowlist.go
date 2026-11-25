// Package allowlist 提供产品无关的命令、路径和参数白名单。
package allowlist

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	ErrDenied  = errors.New("白名单拒绝该输入")
	ErrInvalid = errors.New("白名单规则无效")

	commandPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	argPattern     = regexp.MustCompile(`^[A-Za-z0-9._:=/+@-]{1,256}$`)
)

type PathPolicy struct {
	Roots []string
}

// CommandPolicy 是产品无关的命令名白名单；Names 由调用方注入，框架不硬编码业务命令。
type CommandPolicy struct {
	Names []string
}

func (policy CommandPolicy) Check(name string) error {
	if len(policy.Names) == 0 {
		return ErrInvalid
	}
	if err := CheckCommand(name); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	for _, allowed := range policy.Names {
		allowed = strings.TrimSpace(allowed)
		if CheckCommand(allowed) != nil {
			return ErrInvalid
		}
		if name == allowed {
			return nil
		}
	}
	return ErrDenied
}

// ArgPolicy 是产品无关的参数白名单；Values 由调用方注入。
type ArgPolicy struct {
	Values []string
}

func (policy ArgPolicy) Check(value string) error {
	if len(policy.Values) == 0 {
		return ErrInvalid
	}
	if err := CheckArg(value); err != nil {
		return err
	}
	for _, allowed := range policy.Values {
		if CheckArg(allowed) != nil {
			return ErrInvalid
		}
		if value == allowed {
			return nil
		}
	}
	return ErrDenied
}

func CheckCommand(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `\/`) || !commandPattern.MatchString(name) {
		return ErrDenied
	}
	return nil
}

func CheckDeepLink(raw string, schemes []string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(schemes) == 0 {
		return ErrDenied
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Opaque != "" || parsed.User != nil {
		return ErrDenied
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "javascript" || scheme == "data" || scheme == "file" || scheme == "http" || scheme == "https" || scheme == "vbscript" {
		return ErrDenied
	}
	allowed := false
	for _, candidate := range schemes {
		if strings.ToLower(strings.TrimSpace(candidate)) == scheme {
			allowed = true
			break
		}
	}
	if !allowed || containsDotDot(raw) || containsDotDot(parsed.Path) || containsDotDot(parsed.RawPath) || containsControl(raw) {
		return ErrDenied
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || containsDotDot(decodedPath) {
		return ErrDenied
	}
	if decoded, err := url.PathUnescape(raw); err != nil || containsDotDot(decoded) {
		return ErrDenied
	}
	if containsDangerousNestedScheme(parsed.RawQuery) || containsDangerousNestedScheme(parsed.Fragment) {
		return ErrDenied
	}
	return nil
}

func containsDotDot(value string) bool {
	return strings.Contains(value, "..")
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func containsDangerousNestedScheme(value string) bool {
	if value == "" {
		return false
	}
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return true
	}
	lower := strings.ToLower(decoded)
	for _, scheme := range []string{"javascript:", "data:", "file:", "vbscript:", "http:", "https:"} {
		if strings.Contains(lower, scheme) {
			return true
		}
	}
	return false
}

func CheckArg(value string) error {
	if value == "" || strings.ContainsAny(value, " \t\n\r|&;<>$`\\\"'") || !argPattern.MatchString(value) {
		return ErrDenied
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ErrDenied
		}
	}
	return nil
}

func (policy PathPolicy) Check(path string) error {
	if len(policy.Roots) == 0 {
		return ErrInvalid
	}
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) || containsControl(path) {
		return ErrDenied
	}
	cleaned := filepath.Clean(path)
	info, err := os.Lstat(cleaned)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ErrDenied
	}
	resolved := cleaned
	if err == nil {
		if linked, linkErr := filepath.EvalSymlinks(cleaned); linkErr == nil {
			resolved = linked
		}
	}
	for _, root := range policy.Roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || !filepath.IsAbs(root) {
			return ErrInvalid
		}
		if linked, err := filepath.EvalSymlinks(root); err == nil {
			root = linked
		}
		rel, relErr := filepath.Rel(root, resolved)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return ErrDenied
}
