package model

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidID = errors.New("Framework ID 无效")

type ID string

func ParseID(value string) (ID, error) {
	if len(value) < 1 || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value || hasControl(value) {
		return "", ErrInvalidID
	}
	return ID(value), nil
}

func (id ID) String() string {
	return string(id)
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
