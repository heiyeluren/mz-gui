package credentials

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxKeyLength   = 128
	maxValueLength = 4096
)

var (
	ErrInvalidArgument = errors.New("凭据参数无效")
	ErrNotFound        = errors.New("凭据不存在")
	ErrUnavailable     = errors.New("系统凭据库不可用")

	servicePattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	keyPartPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// Backend 隔离第三方系统凭据库实现，便于领域测试使用内存替身。
type Backend interface {
	Get(service string, key string) (string, error)
	Set(service string, key string, value string) error
	Delete(service string, key string) error
}

type Options struct {
	Backend Backend
	Service string
}

type Store struct {
	backend Backend
	service string
}

func New(options Options) (*Store, error) {
	if options.Backend == nil {
		return nil, ErrInvalidArgument
	}
	service := options.Service
	if !servicePattern.MatchString(service) {
		return nil, ErrInvalidArgument
	}
	return &Store{backend: options.Backend, service: service}, nil
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	if err := validateContextAndKey(ctx, key); err != nil {
		return "", err
	}
	value, err := s.backend.Get(s.service, key)
	if err != nil {
		return "", mapBackendError(err)
	}
	return value, nil
}

func (s *Store) Set(ctx context.Context, key string, value string) error {
	if err := validateContextAndKey(ctx, key); err != nil {
		return err
	}
	if err := validateValue(value); err != nil {
		return err
	}
	return mapBackendError(s.backend.Set(s.service, key, value))
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateContextAndKey(ctx, key); err != nil {
		return err
	}
	return mapBackendError(s.backend.Delete(s.service, key))
}

func validateContextAndKey(ctx context.Context, key string) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(key) == 0 || len(key) > maxKeyLength {
		return ErrInvalidArgument
	}
	parts := strings.Split(key, "/")
	if len(parts) < 2 {
		return ErrInvalidArgument
	}
	for _, part := range parts {
		if !keyPartPattern.MatchString(part) {
			return ErrInvalidArgument
		}
	}
	return nil
}

func validateValue(value string) error {
	if value == "" || len(value) > maxValueLength || !utf8.ValidString(value) {
		return ErrInvalidArgument
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return ErrInvalidArgument
		}
	}
	return nil
}

func mapBackendError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return ErrUnavailable
}
