//go:build windows

package terminal

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
)

type windowsLauncher struct{ termProgram string }

func NewPlatformLauncher(options PlatformLauncherOptions) (Launcher, error) {
	termProgram, err := normalizeTermProgram(options)
	if err != nil {
		return nil, err
	}
	return windowsLauncher{termProgram: termProgram}, nil
}

func (launcher windowsLauncher) Launch(ctx context.Context, request LaunchRequest) (Device, Process, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if request.Multiplexer != nil {
		return nil, nil, errors.New("当前平台不支持 tmux 持久终端")
	}
	program, arguments := windowsShell()
	if request.Executable != "" {
		program = request.Executable
		arguments = append([]string{request.Executable}, request.Arguments...)
	}
	device, err := conpty.New(request.Cols, request.Rows, 0)
	if err != nil {
		return nil, nil, err
	}
	pid, handle, err := device.Spawn(program, arguments, &syscall.ProcAttr{
		Dir: request.CWD,
		Env: mergeProgramEnvironment(allowedEnvironment(launcher.termProgram, "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE", "HOMEPATH", "PATH", "PATHEXT", "SystemRoot", "WINDIR", "COMSPEC", "TEMP", "TMP", "LANG"), request.Environment),
		Sys: &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP},
	})
	if err != nil {
		_ = device.Close()
		return nil, nil, err
	}
	job, err := createTerminalJob(windows.Handle(handle))
	if err != nil {
		_ = windows.TerminateProcess(windows.Handle(handle), 1)
		_ = windows.CloseHandle(windows.Handle(handle))
		_ = device.Close()
		return nil, nil, err
	}
	return device, &windowsProcess{pid: pid, process: windows.Handle(handle), job: job, device: device}, nil
}

func windowsShell() (string, []string) {
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		return path, []string{path, "-NoLogo"}
	}
	path, err := exec.LookPath("cmd.exe")
	if err != nil {
		return "cmd.exe", []string{"cmd.exe"}
	}
	return path, []string{path}
}

func createTerminalJob(process windows.Handle) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
	); err != nil {
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
	pid     int
	process windows.Handle
	job     windows.Handle
	device  *conpty.ConPty
	close   sync.Once
}

func (process *windowsProcess) PID() int { return process.pid }

func (process *windowsProcess) Wait() (int, error) {
	result, err := windows.WaitForSingleObject(process.process, windows.INFINITE)
	if err != nil {
		process.closeHandles()
		return -1, err
	}
	if result != windows.WAIT_OBJECT_0 {
		process.closeHandles()
		return -1, errors.New("等待 ConPTY 进程失败")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process.process, &code); err != nil {
		process.closeHandles()
		return -1, err
	}
	process.closeHandles()
	return int(code), nil
}

func (process *windowsProcess) Interrupt() error {
	_, err := process.device.Write([]byte{3})
	return err
}

func (process *windowsProcess) KillTree() error {
	err := windows.TerminateJobObject(process.job, 137)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil
	}
	return err
}

func (process *windowsProcess) closeHandles() {
	process.close.Do(func() {
		_ = windows.CloseHandle(process.process)
		_ = windows.CloseHandle(process.job)
	})
}
