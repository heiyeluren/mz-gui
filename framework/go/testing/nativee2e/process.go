package nativee2e

import (
	"context"
	"errors"
	"os/exec"
	"sync"
)

type managedProcess struct {
	command *exec.Cmd
	stdout  *boundedBuffer
	stderr  *boundedBuffer
	done    chan struct{}
	mu      sync.Mutex
	err     error
	exit    *int
}

func startProcess(ctx context.Context, executable string, config Config) (*managedProcess, error) {
	cmd := exec.CommandContext(ctx, executable, config.ArtifactArgs...)
	cmd.Dir = config.WorkingDir
	cmd.Env = mergedEnvironment(config.ArtifactEnv)
	process := &managedProcess{
		command: cmd,
		stdout:  newBoundedBuffer(maximumCapturedOutput),
		stderr:  newBoundedBuffer(maximumCapturedOutput),
		done:    make(chan struct{}),
	}
	cmd.Stdout = process.stdout
	cmd.Stderr = process.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		process.mu.Lock()
		process.err = err
		if cmd.ProcessState != nil {
			code := cmd.ProcessState.ExitCode()
			process.exit = &code
		}
		process.mu.Unlock()
		close(process.done)
	}()
	return process, nil
}

func (process *managedProcess) PID() int {
	if process == nil || process.command == nil || process.command.Process == nil {
		return 0
	}
	return process.command.Process.Pid
}

func (process *managedProcess) exited() (bool, error) {
	select {
	case <-process.done:
		process.mu.Lock()
		defer process.mu.Unlock()
		return true, process.err
	default:
		return false, nil
	}
}

func (process *managedProcess) stop() bool {
	if process == nil || process.command == nil || process.command.Process == nil {
		return false
	}
	stopped := false
	if exited, _ := process.exited(); !exited {
		stopped = true
		_ = process.command.Process.Kill()
	}
	<-process.done
	return stopped
}

func (process *managedProcess) exitCode() *int {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.exit == nil {
		return nil
	}
	value := *process.exit
	return &value
}

func processExitError(process *managedProcess) error {
	exited, err := process.exited()
	if !exited {
		return nil
	}
	if err == nil {
		return errors.New("桌面进程已提前退出")
	}
	return err
}
