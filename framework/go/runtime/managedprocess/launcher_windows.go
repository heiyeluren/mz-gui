//go:build windows

package managedprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type platformLauncher struct{}

func (platformLauncher) Launch(spec Spec) (process, error) {
	command := exec.Command(spec.Executable, spec.Arguments...)
	command.Dir = spec.Directory
	command.Env = mergedEnvironment(spec.Environment)
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	// 先加入 Job 再执行入口，防止入口在 Job 关联前创建不受管后代。
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
	if err := command.Start(); err != nil {
		return nil, err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(command.Process.Pid))
	if err != nil {
		return nil, errors.Join(err, stopSuspendedCommand(command))
	}
	job, err := createJob(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.Join(err, stopSuspendedCommand(command))
	}
	if err := resumeManagedProcess(uint32(command.Process.Pid)); err != nil {
		cleanupErr := stopSuspendedCommand(command)
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(job)
		return nil, errors.Join(err, cleanupErr)
	}
	return &windowsProcess{command: command, handle: handle, job: job}, nil
}

func resumeManagedProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			_, resumeErr := windows.ResumeThread(thread)
			return errors.Join(resumeErr, windows.CloseHandle(thread))
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			return fmt.Errorf("查找挂起进程 %d 的入口线程: %w", pid, err)
		}
	}
}

func stopSuspendedCommand(command *exec.Cmd) error {
	killErr := command.Process.Kill()
	waited := make(chan struct{})
	go func() { _ = command.Wait(); close(waited) }()
	select {
	case <-waited:
		return killErr
	case <-time.After(defaultForceKillWait):
		return errors.Join(killErr, ErrTerminationTimeout)
	}
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
	handle  windows.Handle
	job     windows.Handle
	close   sync.Once
	mu      sync.Mutex
}

// Windows JOBOBJECT_BASIC_ACCOUNTING_INFORMATION 的固定布局。
type jobAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func (process *windowsProcess) PID() int { return process.command.Process.Pid }
func (process *windowsProcess) Wait() (int, error) {
	err := process.command.Wait()
	code := -1
	if process.command.ProcessState != nil {
		code = process.command.ProcessState.ExitCode()
	}
	return code, err
}
func (process *windowsProcess) Interrupt() error { return process.KillTree() }
func (process *windowsProcess) KillTree() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(process.job, 137)
}

func (process *windowsProcess) WaitTree(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		process.mu.Lock()
		if process.job == 0 {
			process.mu.Unlock()
			return nil
		}
		info := jobAccountingInformation{}
		err := windows.QueryInformationJobObject(process.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		process.mu.Unlock()
		if err != nil {
			return err
		}
		if info.ActiveProcesses == 0 {
			process.closeHandles()
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrTerminationTimeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (process *windowsProcess) closeHandles() {
	process.close.Do(func() {
		process.mu.Lock()
		defer process.mu.Unlock()
		_ = windows.CloseHandle(process.handle)
		_ = windows.CloseHandle(process.job)
		process.handle = 0
		process.job = 0
	})
}
