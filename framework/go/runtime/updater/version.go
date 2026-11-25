package updater

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalidVersion = errors.New("语义版本无效")

var semanticVersionPattern = regexp.MustCompile(`^(?:v)?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type semanticVersion struct {
	major, minor, patch string
	prerelease          []string
}

func CompareVersions(left, right string) (int, error) {
	leftVersion, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	rightVersion, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]string{{leftVersion.major, rightVersion.major}, {leftVersion.minor, rightVersion.minor}, {leftVersion.patch, rightVersion.patch}} {
		if comparison := compareNumericIdentifier(pair[0], pair[1]); comparison != 0 {
			return comparison, nil
		}
	}
	return comparePrerelease(leftVersion.prerelease, rightVersion.prerelease), nil
}

func parseVersion(value string) (semanticVersion, error) {
	matches := semanticVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil {
		return semanticVersion{}, ErrInvalidVersion
	}
	prerelease := []string(nil)
	if matches[4] != "" {
		prerelease = strings.Split(matches[4], ".")
		for _, identifier := range prerelease {
			if isNumeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return semanticVersion{}, ErrInvalidVersion
			}
		}
	}
	return semanticVersion{major: matches[1], minor: matches[2], patch: matches[3], prerelease: prerelease}, nil
}

func comparePrerelease(left, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		leftNumeric, rightNumeric := isNumeric(left[index]), isNumeric(right[index])
		switch {
		case leftNumeric && rightNumeric:
			if comparison := compareNumericIdentifier(left[index], right[index]); comparison != 0 {
				return comparison
			}
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		default:
			if left[index] < right[index] {
				return -1
			}
			if left[index] > right[index] {
				return 1
			}
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

func compareNumericIdentifier(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
