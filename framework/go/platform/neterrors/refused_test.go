package neterrors

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

func TestConnectionRefusedUsesNativeSocketError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	c, err := net.DialTimeout("tcp", address, time.Second)
	if c != nil {
		c.Close()
		t.Fatal("测试端口意外被占用")
	}
	if !ConnectionRefused(fmt.Errorf("连接失败: %w", err)) {
		t.Fatalf("未识别本机套接字错误: %v", err)
	}
	for _, other := range []error{nil, context.Canceled, context.DeadlineExceeded, os.ErrPermission} {
		if ConnectionRefused(other) {
			t.Fatalf("不能把 %v 当成空闲端口", other)
		}
	}
}
