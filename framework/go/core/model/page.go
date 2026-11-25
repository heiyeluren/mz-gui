package model

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidPage = errors.New("Framework 分页参数无效")

type PageRequest struct {
	PageSize int    `json:"page_size"`
	Cursor   string `json:"cursor,omitempty"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func NewPageRequest(pageSize int, cursor string, maximum int) (PageRequest, error) {
	if maximum < 1 || pageSize < 1 || pageSize > maximum || !validCursor(cursor) {
		return PageRequest{}, ErrInvalidPage
	}
	return PageRequest{PageSize: pageSize, Cursor: cursor}, nil
}

func NewPage[T any](items []T, nextCursor string) (Page[T], error) {
	if !validCursor(nextCursor) {
		return Page[T]{}, ErrInvalidPage
	}
	return Page[T]{Items: append([]T(nil), items...), NextCursor: nextCursor, HasMore: nextCursor != ""}, nil
}

func validCursor(value string) bool {
	return len(value) <= 1024 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !hasControl(value)
}
