package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	taskv1 "github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/gen/task/v1"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAgentdTaskSurvivesDesktopAndAgentdRestart(t *testing.T) {
	runDir, err := os.MkdirTemp("", "mzgui-agentd-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	binary := filepath.Join(t.TempDir(), "showcase-agentd")
	build := exec.Command("go", "build", "-o", binary, "./agentd")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建真实 agentd: %v\n%s", err, output)
	}
	agent := startAgentd(t, binary, runDir)
	endpoint := local.Endpoint{Name: "showcase-agentd", RunDir: runDir}
	desktop := dialAgentEventually(t, endpoint, agent)
	started, err := desktop.StartTask(context.Background(), `{"source":"integration"}`, "desktop-close-task")
	if err != nil {
		t.Fatal(err)
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := desktop.Close(closeContext); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	time.Sleep(450 * time.Millisecond)
	if agent.exited() {
		t.Fatalf("桌面断开后 agentd 意外退出: %s", agent.logs())
	}

	reconnected := dialAgentEventually(t, endpoint, agent)
	completed := waitTaskState(t, reconnected, started.ID, taskengine.StateSucceeded)
	if !strings.Contains(completed.ResultSummaryJSON, "completed") {
		t.Fatalf("任务结果未持久化: %#v", completed)
	}
	assertSessionRequired(t, reconnected, started.ID)
	second, err := reconnected.StartTask(context.Background(), `{}`, "desktop-cancel-task")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := reconnected.CancelTask(context.Background(), second.ID, "integration cancel", "cancel-second-task")
	if err != nil || cancelled.State != string(taskengine.StateCancelled) {
		t.Fatalf("取消任务失败: task=%#v err=%v", cancelled, err)
	}
	if err := reconnected.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	agent.stop(t)

	restarted := startAgentd(t, binary, runDir)
	afterRestart := dialAgentEventually(t, endpoint, restarted)
	defer func() { _ = afterRestart.Close(context.Background()) }()
	restored, err := afterRestart.GetTask(context.Background(), started.ID)
	if err != nil || restored.State != string(taskengine.StateSucceeded) || !strings.Contains(restored.ResultSummaryJSON, "completed") {
		t.Fatalf("agentd 重启后恢复失败: task=%#v err=%v", restored, err)
	}
	restoredCancel, err := afterRestart.GetTask(context.Background(), second.ID)
	if err != nil || restoredCancel.State != string(taskengine.StateCancelled) {
		t.Fatalf("取消状态未跨重启恢复: task=%#v err=%v", restoredCancel, err)
	}
}

func TestSingleAndMultiProcessBackendsShareDesktopUseCase(t *testing.T) {
	backend, err := newLocalTaskBackend(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close(context.Background()) }()
	service := &DesktopService{backend: backend}
	started, err := service.StartTask(`{"source":"inprocess"}`)
	if err != nil {
		t.Fatal(err)
	}
	var completed TaskSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		completed, err = service.GetTask(started.ID)
		if err != nil {
			t.Fatal(err)
		}
		if completed.State == string(taskengine.StateSucceeded) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if completed.State != string(taskengine.StateSucceeded) || !strings.Contains(completed.ResultSummaryJSON, "completed") {
		t.Fatalf("同一 DesktopService 的进程内适配失败: %#v", completed)
	}
}

func waitTaskState(t *testing.T, client *agentClient, taskID string, expected taskengine.State) TaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var task TaskSnapshot
	for time.Now().Before(deadline) {
		var err error
		task, err = client.GetTask(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.State == string(expected) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待任务状态 %s 超时: %#v", expected, task)
	return TaskSnapshot{}
}

func assertSessionRequired(t *testing.T, client *agentClient, taskID string) {
	t.Helper()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("mzgui-session-id", client.sessionID, "mzgui-session-token", "invalid-token-that-is-long-enough-123456"))
	_, err := client.tasks.GetTask(ctx, &taskv1.GetTaskRequest{TaskId: taskID})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("无效 session 必须拒绝: %v", err)
	}
}

type agentdProcess struct {
	command *exec.Cmd
	log     bytes.Buffer
	done    chan struct{}
}

func startAgentd(t *testing.T, binary, runDir string) *agentdProcess {
	t.Helper()
	process := &agentdProcess{command: exec.Command(binary, "-run-dir", runDir), done: make(chan struct{})}
	process.command.Stdout = &process.log
	process.command.Stderr = &process.log
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = process.command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		if !process.exited() {
			process.stop(t)
		}
	})
	return process
}

func (process *agentdProcess) exited() bool {
	select {
	case <-process.done:
		return true
	default:
		return false
	}
}

func (process *agentdProcess) logs() string {
	return process.log.String()
}

func (process *agentdProcess) stop(t *testing.T) {
	t.Helper()
	if process.exited() {
		return
	}
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Logf("发送 agentd 退出信号: %v", err)
	}
	select {
	case <-process.done:
	case <-time.After(3 * time.Second):
		_ = process.command.Process.Kill()
		<-process.done
		t.Errorf("agentd 未在退出期限内停止: %s", process.logs())
	}
}

func dialAgentEventually(t *testing.T, endpoint local.Endpoint, process *agentdProcess) *agentClient {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var lastError error
	for time.Now().Before(deadline) {
		if process.exited() {
			t.Fatalf("agentd 启动失败: %s", process.logs())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		client, err := newAgentClient(ctx, endpoint)
		cancel()
		if err == nil {
			return client
		}
		lastError = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("连接 agentd 超时: %v\n%s", lastError, process.logs())
	return nil
}
