package processrun

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStreamHelper(t *testing.T) {
	if os.Getenv("MZ_STREAM_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println("reply:" + scanner.Text())
		if scanner.Text() == "exit" {
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func TestStreamNaturalExitRetainsLastOutput(t *testing.T) {
	s, err := StartStream(context.Background(), StreamSpec{Executable: os.Args[0], Arguments: []string{"-test.run=^TestStreamHelper$"}, Environment: append(os.Environ(), "MZ_STREAM_HELPER=1")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = fmt.Fprintln(s.Input, "exit")
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("进程未退出")
	}
	line, err := bufio.NewReader(s.Output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "reply:exit" {
		t.Fatalf("自然退出丢失尾帧: %q %v", line, err)
	}
}

func TestStreamMultipleWritesAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := StartStream(ctx, StreamSpec{Executable: os.Args[0], Arguments: []string{"-test.run=^TestStreamHelper$"}, Environment: append(os.Environ(), "MZ_STREAM_HELPER=1"), Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.PID() <= 0 {
		t.Fatal("缺少进程身份")
	}
	reader := bufio.NewReader(s.Output)
	for _, message := range []string{"第一轮", "第二轮"} {
		if _, err := fmt.Fprintln(s.Input, message); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "reply:"+message {
			t.Fatalf("双向传输失败: %q %v", line, err)
		}
		select {
		case <-s.Done():
			t.Fatal("一轮完成不能关闭进程")
		default:
		}
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("取消没有回收进程")
	}
}

func TestStreamRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := StartStream(ctx, StreamSpec{Executable: os.Args[0]}); err == nil {
		t.Fatal("取消后不能启动进程")
	}
}
