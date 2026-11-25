package remotedebug

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"time"
)

type Client struct {
	conn   net.Conn
	reader *bufio.Reader
}

func Dial(ctx context.Context, addr, password string) (*Client, error) {
	dialer := net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, reader: bufio.NewReaderSize(conn, 64*1024)}
	if err := writeMessage(conn, request{Op: "auth", Password: password}); err != nil {
		conn.Close()
		return nil, err
	}
	var resp response
	if err := readMessage(client.reader, &resp); err != nil {
		conn.Close()
		return nil, err
	}
	if !resp.OK {
		conn.Close()
		return nil, ErrAuthFailed
	}
	return client, nil
}

func (client *Client) Run(ctx context.Context, command string, timeout time.Duration) (Result, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if err := writeMessage(client.conn, request{Op: "run", Cmd: command, TimeoutSec: int(timeout.Seconds())}); err != nil {
		return Result{}, err
	}
	var resp response
	if err := readMessage(client.reader, &resp); err != nil {
		return Result{}, err
	}
	if !resp.OK {
		return Result{}, fmt.Errorf("%w: %s", ErrProtocol, resp.Error)
	}
	return Result{ExitCode: resp.ExitCode, Stdout: resp.Stdout, Stderr: resp.Stderr}, nil
}

// RunChecked 保留远端 stdout/stderr，并把任何非零退出转换为可断言错误。
func (client *Client) RunChecked(ctx context.Context, command string, timeout time.Duration) (Result, error) {
	result, err := client.Run(ctx, command, timeout)
	if err != nil {
		return result, err
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("%w: exit_code=%d", ErrRemoteCommandFailed, result.ExitCode)
	}
	return result, nil
}

func (client *Client) Get(ctx context.Context, path string) ([]byte, error) {
	if err := writeMessage(client.conn, request{Op: "get", Path: path}); err != nil {
		return nil, err
	}
	var resp response
	if err := readMessage(client.reader, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%w: %s", ErrProtocol, resp.Error)
	}
	return []byte(resp.Content), nil
}

func (client *Client) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}
