package remotedebug

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	DefaultPort     = 1983
	DefaultPassword = "mzgui-debug-1983"
	maxMessageBytes = 8 << 20
)

var (
	ErrAuthFailed = errors.New("远程调试配对失败")
	ErrNotAuthed  = errors.New("尚未配对")
	ErrProtocol   = errors.New("远程调试协议无效")
	// ErrRemoteCommandFailed 让原生 E2E 调度端可靠识别设备侧非零退出。
	ErrRemoteCommandFailed = errors.New("远端命令执行失败")
)

type request struct {
	Op         string `json:"op"`
	Password   string `json:"password,omitempty"`
	Cmd        string `json:"cmd,omitempty"`
	Path       string `json:"path,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Content  string `json:"content,omitempty"`
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func writeMessage(conn net.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	_, err = conn.Write(append(data, '\n'))
	return err
}

func readMessage(reader *bufio.Reader, dest any) error {
	data, err := reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: 连接关闭", ErrProtocol)
		}
		return err
	}
	if len(data) > maxMessageBytes {
		return fmt.Errorf("%w: 报文过大", ErrProtocol)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return nil
}
