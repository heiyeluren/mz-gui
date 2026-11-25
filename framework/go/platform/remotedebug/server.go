package remotedebug

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type RunnerFunc func(ctx context.Context, command string) Result

type Server struct {
	Addr     string
	Password string
	Runner   RunnerFunc
}

func (server *Server) password() string {
	if strings.TrimSpace(server.Password) != "" {
		return server.Password
	}
	return DefaultPassword
}

func (server *Server) runner() RunnerFunc {
	if server.Runner != nil {
		return server.Runner
	}
	return RunCommand
}

func (server *Server) Start(ctx context.Context) (string, func(), error) {
	addr := strings.TrimSpace(server.Addr)
	if addr == "" {
		addr = fmt.Sprintf("0.0.0.0:%d", DefaultPort)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.handle(ctx, conn)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	stop := func() {
		_ = listener.Close()
		<-done
	}
	return listener.Addr().String(), stop, nil
}

func (server *Server) handle(parent context.Context, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReaderSize(conn, 64*1024)
	authed := false
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Minute)); err != nil {
			return
		}
		var req request
		if err := readMessage(reader, &req); err != nil {
			return
		}
		switch strings.ToLower(strings.TrimSpace(req.Op)) {
		case "auth":
			if req.Password != server.password() {
				_ = writeMessage(conn, response{OK: false, Error: ErrAuthFailed.Error()})
				return
			}
			authed = true
			_ = writeMessage(conn, response{OK: true})
		case "run":
			if !authed {
				_ = writeMessage(conn, response{OK: false, Error: ErrNotAuthed.Error()})
				return
			}
			timeout := time.Duration(req.TimeoutSec) * time.Second
			if timeout <= 0 {
				timeout = 10 * time.Minute
			}
			runCtx, cancel := context.WithTimeout(parent, timeout)
			result := server.runner()(runCtx, req.Cmd)
			cancel()
			_ = writeMessage(conn, response{
				OK:       true,
				ExitCode: result.ExitCode,
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
			})
		case "get":
			if !authed {
				_ = writeMessage(conn, response{OK: false, Error: ErrNotAuthed.Error()})
				return
			}
			data, err := os.ReadFile(req.Path)
			if err != nil {
				_ = writeMessage(conn, response{OK: false, Error: err.Error()})
				continue
			}
			if len(data) > maxMessageBytes {
				_ = writeMessage(conn, response{OK: false, Error: "文件过大"})
				continue
			}
			_ = writeMessage(conn, response{OK: true, Content: string(data)})
		default:
			_ = writeMessage(conn, response{OK: false, Error: ErrProtocol.Error()})
			return
		}
	}
}
