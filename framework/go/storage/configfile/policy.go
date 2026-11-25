package configfile

import (
	"errors"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var ErrPolicyLocked = errors.New("配置策略锁定字段不可修改")

func EnforceTOMLLockedKeys(before []byte, after []byte, lockedKeys []string) error {
	if len(lockedKeys) == 0 {
		return nil
	}
	var beforeMap map[string]any
	var afterMap map[string]any
	if err := toml.Unmarshal(before, &beforeMap); err != nil {
		return err
	}
	if err := toml.Unmarshal(after, &afterMap); err != nil {
		return err
	}
	for _, key := range lockedKeys {
		path := strings.Split(strings.TrimSpace(key), ".")
		if len(path) == 0 || path[0] == "" {
			return ErrInvalidOptions
		}
		beforeValue, beforeOK := lookupPath(beforeMap, path)
		afterValue, afterOK := lookupPath(afterMap, path)
		if beforeOK != afterOK || !reflect.DeepEqual(beforeValue, afterValue) {
			return ErrPolicyLocked
		}
	}
	return nil
}

func lookupPath(values map[string]any, path []string) (any, bool) {
	var current any = values
	for _, part := range path {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
