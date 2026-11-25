package webviewruntime

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidRuntimeVersion = errors.New("WebView 运行时版本无效")

type VersionClass string

const (
	VersionOK       VersionClass = "ok"
	VersionOutdated VersionClass = "outdated"
)

func ClassifyVersion(current, minimum string) (VersionClass, error) {
	currentParts, err := parseRuntimeVersion(current)
	if err != nil {
		return "", err
	}
	minimumParts, err := parseRuntimeVersion(minimum)
	if err != nil {
		return "", err
	}
	if compareVersionParts(currentParts, minimumParts) < 0 {
		return VersionOutdated, nil
	}
	return VersionOK, nil
}

func parseRuntimeVersion(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidRuntimeVersion
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return nil, ErrInvalidRuntimeVersion
	}
	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return nil, ErrInvalidRuntimeVersion
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, ErrInvalidRuntimeVersion
		}
		parsed = append(parsed, number)
	}
	return parsed, nil
}

func compareVersionParts(left, right []int) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}
