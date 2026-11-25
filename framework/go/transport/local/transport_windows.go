//go:build windows

package local

import (
	"context"
	"fmt"
	"net"
	"os/user"

	"github.com/Microsoft/go-winio"
)

func listen(endpoint Endpoint) (net.Listener, error) {
	current, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("读取当前 Windows 用户: %w", err)
	}
	securityDescriptor := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", current.Uid)
	return winio.ListenPipe(PipeName(endpoint.Name, current.Uid), &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor,
		MessageMode:        false,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	current, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("读取当前 Windows 用户: %w", err)
	}
	return winio.DialPipeContext(ctx, PipeName(endpoint.Name, current.Uid))
}
