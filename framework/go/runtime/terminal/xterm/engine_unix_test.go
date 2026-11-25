//go:build !windows

package xterm

import (
	"syscall"
	"testing"
	"time"
)

func TestCloseInterruptsStalledStateWorker(t *testing.T) {
	e := engine(t)
	if err := e.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	writing := make(chan error, 1)
	go func() { writing <- e.Write([]byte("等待解析")) }()
	time.Sleep(30 * time.Millisecond)
	closed := make(chan struct{})
	go func() { e.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("关闭被状态进程拖住")
	}
	select {
	case <-writing:
	case <-time.After(time.Second):
		t.Fatal("写入等待者未释放")
	}
}
