package model

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidVersion = errors.New("Framework 版本无效")
	versionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
)

type Version struct {
	Major      uint64 `json:"major"`
	Minor      uint64 `json:"minor"`
	Patch      uint64 `json:"patch"`
	Prerelease string `json:"prerelease,omitempty"`
	Build      string `json:"build,omitempty"`
}

func ParseVersion(value string) (Version, error) {
	matches := versionPattern.FindStringSubmatch(value)
	if matches == nil || !validPrerelease(matches[4]) {
		return Version{}, ErrInvalidVersion
	}
	major, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return Version{}, ErrInvalidVersion
	}
	minor, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return Version{}, ErrInvalidVersion
	}
	patch, err := strconv.ParseUint(matches[3], 10, 64)
	if err != nil {
		return Version{}, ErrInvalidVersion
	}
	return Version{Major: major, Minor: minor, Patch: patch, Prerelease: matches[4], Build: matches[5]}, nil
}

func (version Version) String() string {
	value := strconv.FormatUint(version.Major, 10) + "." + strconv.FormatUint(version.Minor, 10) + "." + strconv.FormatUint(version.Patch, 10)
	if version.Prerelease != "" {
		value += "-" + version.Prerelease
	}
	if version.Build != "" {
		value += "+" + version.Build
	}
	return value
}

func (version Version) Compare(other Version) int {
	for _, pair := range [][2]uint64{{version.Major, other.Major}, {version.Minor, other.Minor}, {version.Patch, other.Patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return comparePrerelease(version.Prerelease, other.Prerelease)
}

func (version Version) CompatibleWith(required Version) bool {
	if version.Major != required.Major || version.Compare(required) < 0 {
		return false
	}
	return version.Major != 0 || version.Minor == required.Minor
}

func validPrerelease(value string) bool {
	if value == "" {
		return true
	}
	for _, item := range strings.Split(value, ".") {
		if len(item) > 1 && item[0] == '0' && numeric(item) {
			return false
		}
	}
	return true
}

func comparePrerelease(left string, right string) int {
	if left == right {
		return 0
	}
	if left == "" {
		return 1
	}
	if right == "" {
		return -1
	}
	leftItems, rightItems := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(leftItems) && index < len(rightItems); index++ {
		if leftItems[index] == rightItems[index] {
			continue
		}
		leftNumeric, rightNumeric := numeric(leftItems[index]), numeric(rightItems[index])
		switch {
		case leftNumeric && rightNumeric:
			return compareNumericIdentifier(leftItems[index], rightItems[index])
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case leftItems[index] < rightItems[index]:
			return -1
		default:
			return 1
		}
	}
	if len(leftItems) < len(rightItems) {
		return -1
	}
	return 1
}

func numeric(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}

func compareNumericIdentifier(left string, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	if left < right {
		return -1
	}
	return 1
}
