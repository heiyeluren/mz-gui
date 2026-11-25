package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/updater"
)

const maximumReleaseInputBytes = 1 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguireleasesign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	payloadPath := flags.String("payload", "", "待签名 updater Payload JSON")
	privateKeyPath := flags.String("private-key", "", "Ed25519 私钥文件，必须为 0600 且不能是符号链接")
	keyID := flags.String("key-id", "", "发布密钥标识")
	manifestPath := flags.String("manifest", "", "签名清单输出路径")
	trustSpecPath := flags.String("trust-spec", "", "可选的构建期公钥规格输出路径")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*payloadPath) == "" || strings.TrimSpace(*privateKeyPath) == "" || strings.TrimSpace(*keyID) == "" || strings.TrimSpace(*manifestPath) == "" {
		fmt.Fprintln(stderr, "必须提供 --payload、--private-key、--key-id 和 --manifest")
		return 2
	}
	payload, err := readPayload(*payloadPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取发布内容失败: %v\n", err)
		return 1
	}
	privateKey, err := readPrivateKey(*privateKeyPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取发布私钥失败: %v\n", err)
		return 1
	}
	manifest, err := updater.Sign(payload, *keyID, privateKey)
	if err != nil {
		fmt.Fprintf(stderr, "签名发布清单失败: %v\n", err)
		return 1
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if _, err := updater.ParseAndVerify(manifest, updater.VerifyPolicy{
		Product: payload.Product,
		Channel: payload.Channel,
		Keys:    map[string]ed25519.PublicKey{*keyID: publicKey},
	}); err != nil {
		fmt.Fprintf(stderr, "签名后回验失败: %v\n", err)
		return 1
	}
	if err := writeAtomic(*manifestPath, append(manifest, '\n'), 0o600); err != nil {
		fmt.Fprintf(stderr, "写入签名清单失败: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*trustSpecPath) != "" {
		trustSpec := *keyID + "=" + base64.StdEncoding.EncodeToString(publicKey) + "\n"
		if err := writeAtomic(*trustSpecPath, []byte(trustSpec), 0o600); err != nil {
			fmt.Fprintf(stderr, "写入公钥规格失败: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "已生成签名清单: %s\n", filepath.Clean(*manifestPath))
	if strings.TrimSpace(*trustSpecPath) != "" {
		fmt.Fprintf(stdout, "已生成构建公钥规格: %s\n", filepath.Clean(*trustSpecPath))
	}
	return 0
}

func readPayload(path string) (updater.Payload, error) {
	data, err := readBoundedRegularFile(path, maximumReleaseInputBytes, false)
	if err != nil {
		return updater.Payload{}, err
	}
	var payload updater.Payload
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return updater.Payload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return updater.Payload{}, errors.New("发布内容包含尾随数据")
	}
	return payload, nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := readBoundedRegularFile(path, 1024, true)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("私钥必须是 64 字节 Ed25519 私钥的 Base64 编码")
	}
	return ed25519.PrivateKey(append([]byte(nil), decoded...)), nil
}

func readBoundedRegularFile(path string, maximum int64, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("文件不能是符号链接")
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum {
		return nil, errors.New("文件类型或大小无效")
	}
	if private && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("私钥文件权限必须限制为 0600")
	}
	return os.ReadFile(path)
}

func writeAtomic(path string, data []byte, mode os.FileMode) (err error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == string(filepath.Separator) {
		return errors.New("输出路径无效")
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("输出路径不能是符号链接")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".mzgui-release-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
