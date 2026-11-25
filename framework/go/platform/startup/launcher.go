package startup

import (
	"context"
	"fmt"
	"os"
	"runtime"
)

type LauncherOptions struct {
	SidecarName       string
	GOOS              string
	DesktopExecutable func() (string, error)
	StartDetached     func(context.Context, string) error
}

type Launcher struct {
	options LauncherOptions
}

func NewLauncher(options LauncherOptions) *Launcher {
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.DesktopExecutable == nil {
		options.DesktopExecutable = os.Executable
	}
	if options.StartDetached == nil {
		options.StartDetached = startDetached
	}
	return &Launcher{options: options}
}

func (launcher *Launcher) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if launcher == nil {
		return ErrInvalidSidecarName
	}
	desktop, err := launcher.options.DesktopExecutable()
	if err != nil {
		return fmt.Errorf("读取桌面进程路径: %w", err)
	}
	sidecar, err := ResolveSiblingExecutable(desktop, launcher.options.SidecarName, launcher.options.GOOS)
	if err != nil {
		return err
	}
	if err := launcher.options.StartDetached(ctx, sidecar); err != nil {
		return fmt.Errorf("启动 sidecar %s: %w", launcher.options.SidecarName, err)
	}
	return nil
}
