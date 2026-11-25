//go:build windows

package processrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func launch(ctx context.Context, spec Spec) (process, error) {
	command := exec.Command(spec.Executable, spec.Arguments...)
	command.Dir = spec.Directory
	command.Env = append([]string(nil), spec.Environment...)
	command.Stdin = bytes.NewReader(spec.Input)
	if spec.stdin != nil {
		command.Stdin = spec.stdin
	}
	command.Stdout = spec.stdout
	command.Stderr = spec.stderr
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
	if err := command.Start(); err != nil {
		return nil, err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
	if err != nil {
		return nil, errors.Join(err, cleanupSuspendedProcess(command, 0, 0))
	}
	job, err := createJob(handle)
	if err != nil {
		return nil, errors.Join(err, cleanupSuspendedProcess(command, handle, 0))
	}
	if err := resumeProcessThreads(uint32(command.Process.Pid)); err != nil {
		return nil, errors.Join(err, cleanupSuspendedProcess(command, handle, job))
	}
	return &windowsProcess{command: command, handle: handle, job: job}, nil
}

func createJob(process windows.Handle) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

type windowsProcess struct {
	command *exec.Cmd
	close   sync.Once
	mu      sync.Mutex
	handle  windows.Handle
	job     windows.Handle
}

func (process *windowsProcess) PID() int { return process.command.Process.Pid }

func (process *windowsProcess) Wait() (int, error) {
	err := process.command.Wait()
	if process.command.ProcessState == nil {
		return -1, err
	}
	return process.command.ProcessState.ExitCode(), err
}

func (process *windowsProcess) KillTree() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(process.job, 137)
}

func (process *windowsProcess) Kill() error {
	err := process.command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (process *windowsProcess) Close() error {
	var result error
	process.close.Do(func() {
		process.mu.Lock()
		defer process.mu.Unlock()
		if process.handle != 0 {
			result = errors.Join(result, windows.CloseHandle(process.handle))
			process.handle = 0
		}
		if process.job != 0 {
			result = errors.Join(result, windows.CloseHandle(process.job))
			process.job = 0
		}
	})
	return result
}

func resumeProcessThreads(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	resumed := 0
	for {
		if entry.OwnerProcessID == processID {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			_, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)
			if resumeErr != nil || closeErr != nil {
				return errors.Join(resumeErr, closeErr)
			}
			resumed++
		}
		err = windows.Thread32Next(snapshot, &entry)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return err
		}
	}
	if resumed == 0 {
		return fmt.Errorf("未找到进程 %d 的挂起线程", processID)
	}
	return nil
}

func cleanupSuspendedProcess(command *exec.Cmd, handle, job windows.Handle) error {
	var terminateErr error
	var directErr error
	if job != 0 {
		terminateErr = windows.TerminateJobObject(job, 137)
	}
	if command.Process != nil {
		directErr = command.Process.Kill()
	}
	var closeErr error
	if handle != 0 {
		closeErr = errors.Join(closeErr, windows.CloseHandle(handle))
	}
	if job != 0 {
		closeErr = errors.Join(closeErr, windows.CloseHandle(job))
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	timer := time.NewTimer(terminationWait)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-waited:
	case <-timer.C:
		waitErr = ErrTerminationTimeout
	}
	return errors.Join(terminateErr, directErr, waitErr, closeErr)
}
