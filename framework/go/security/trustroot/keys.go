// Package trustroot 提供构建期只读发布公钥的严格解析能力。
package trustroot

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

var keyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Parse 解析 key_id=base64_public_key;... 格式的公钥集合。
func Parse(spec string) (map[string]ed25519.PublicKey, error) {
	result := make(map[string]ed25519.PublicKey)
	if spec == "" {
		return result, nil
	}
	if strings.TrimSpace(spec) != spec || len(spec) > 64<<10 {
		return nil, errors.New("发布信任根格式无效")
	}
	for _, entry := range strings.Split(spec, ";") {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || !keyIDPattern.MatchString(parts[0]) || parts[1] == "" {
			return nil, errors.New("发布信任根格式无效")
		}
		if _, duplicate := result[parts[0]]; duplicate {
			return nil, errors.New("发布信任根包含重复密钥")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(parts[1])
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, errors.New("发布信任根公钥无效")
		}
		result[parts[0]] = ed25519.PublicKey(append([]byte(nil), decoded...))
	}
	return result, nil
}
