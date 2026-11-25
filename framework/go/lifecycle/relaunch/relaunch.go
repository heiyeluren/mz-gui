// Package relaunch 在旧宿主退出后启动同一可执行文件，不承载产品或实例语义。
package relaunch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const WaitFlag = "--mzgui-relaunch-wait"

// AwaitParent 必须在 GUI 和单实例锁初始化之前调用。普通启动不做等待。
func AwaitParent(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != WaitFlag {
		return nil
	}
	if len(args) != 2 {
		return errors.New("重启等待参数无效")
	}
	pid, err := strconv.Atoi(args[1])
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return errors.New("重启父进程无效")
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return waitParent(bounded, pid)
}

// Start 预启动等待进程；返回的取消函数只终止本次创建的进程，不按名称查杀。
func Start(ctx context.Context, overrides map[string]string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, WaitFlag, strconv.Itoa(os.Getpid()))
	command.Env = environment(os.Environ(), overrides)
	detach(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	go func() { _ = command.Wait() }()
	return func() { _ = command.Process.Kill() }, nil
}

func environment(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		replaced := false
		for name := range overrides {
			if strings.EqualFold(key, name) {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, item)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}
