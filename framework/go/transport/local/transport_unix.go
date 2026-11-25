//go:build darwin || linux

package local

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const maxUnixSocketPathBytes = 100

func listen(endpoint Endpoint) (net.Listener, error) {
	if err := os.MkdirAll(endpoint.RunDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 IPC 运行目录: %w", err)
	}
	if err := os.Chmod(endpoint.RunDir, 0o700); err != nil {
		return nil, fmt.Errorf("设置 IPC 运行目录权限: %w", err)
	}
	socketPath, err := darwinSocketPath(endpoint)
	if err != nil {
		return nil, err
	}
	if socketDir := filepath.Dir(socketPath); socketDir != endpoint.RunDir {
		if err := os.MkdirAll(socketDir, 0o700); err != nil {
			return nil, fmt.Errorf("创建 IPC 短路径目录: %w", err)
		}
		if err := os.Chmod(socketDir, 0o700); err != nil {
			return nil, fmt.Errorf("设置 IPC 短路径目录权限: %w", err)
		}
	}
	if err := clearStaleSocket(socketPath); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("监听本机 UDS: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("设置本机 UDS 权限: %w", err)
	}
	return &cleanupListener{Listener: listener, endpoint: socketPath}, nil
}

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	socketPath, err := darwinSocketPath(endpoint)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, "unix", socketPath)
}

func darwinSocketPath(endpoint Endpoint) (string, error) {
	socketPath := filepath.Join(endpoint.RunDir, endpoint.Name+".sock")
	if len([]byte(socketPath)) <= maxUnixSocketPathBytes {
		return socketPath, nil
	}
	sum := sha256.Sum256([]byte(endpoint.RunDir + "\x00" + endpoint.Name))
	shortDir := filepath.Join(os.TempDir(), fmt.Sprintf("mz-gui-ipc-%d", os.Getuid()), fmt.Sprintf("%x", sum[:8]))
	socketPath = filepath.Join(shortDir, endpoint.Name+".sock")
	if len([]byte(socketPath)) > maxUnixSocketPathBytes {
		return "", fmt.Errorf("UDS 短路径仍过长: %d 字节", len([]byte(socketPath)))
	}
	return socketPath, nil
}

func clearStaleSocket(endpoint string) error {
	info, err := os.Lstat(endpoint)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查本机 UDS: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("本机 UDS 不能是符号链接")
	}
	connection, dialErr := net.DialTimeout("unix", endpoint, 100*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		return fmt.Errorf("本机 IPC 服务已在运行")
	}
	if err := os.Remove(endpoint); err != nil {
		return fmt.Errorf("清理失效本机 UDS: %w", err)
	}
	return nil
}

type cleanupListener struct {
	net.Listener
	endpoint string
}

func (listener *cleanupListener) Close() error {
	err := listener.Listener.Close()
	removeErr := os.Remove(listener.endpoint)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(err, removeErr)
}
