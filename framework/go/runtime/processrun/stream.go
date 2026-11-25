package processrun

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StreamSpec 启动持续双向通信的受管子进程，调用方负责协议与输出消费。
type StreamSpec struct {
	Executable  string
	Arguments   []string
	Environment []string
	Directory   string
}

type Stream struct {
	Input  io.WriteCloser
	Output io.ReadCloser
	pid    int
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *Stream) PID() int              { return s.pid }
func (s *Stream) Done() <-chan struct{} { return s.done }
func (s *Stream) Close() error {
	s.once.Do(func() { s.cancel(); _ = s.Input.Close(); _ = s.Output.Close() })
	select {
	case <-s.done:
		return nil
	case <-time.After(terminationWait):
		return ErrTerminationTimeout
	}
}

func StartStream(ctx context.Context, spec StreamSpec) (*Stream, error) {
	if ctx == nil || !filepath.IsAbs(spec.Executable) {
		return nil, ErrInvalidSpec
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inReader, inWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outReader, outWriter, err := os.Pipe()
	if err != nil {
		_ = inReader.Close()
		_ = inWriter.Close()
		return nil, err
	}
	closePipes := func() { _ = inReader.Close(); _ = inWriter.Close(); _ = outReader.Close(); _ = outWriter.Close() }
	lifetime, cancel := context.WithCancel(ctx)
	child, err := launch(lifetime, Spec{Executable: spec.Executable, Arguments: spec.Arguments, Environment: spec.Environment,
		Directory: spec.Directory, stdin: inReader, stdout: outWriter, stderr: io.Discard})
	_ = inReader.Close()
	_ = outWriter.Close()
	if err != nil {
		cancel()
		closePipes()
		return nil, err
	}
	identity, ok := child.(interface{ PID() int })
	if !ok {
		cancel()
		_ = child.KillTree()
		_ = child.Kill()
		_, _ = child.Wait()
		_ = child.Close()
		closePipes()
		return nil, ErrInvalidSpec
	}
	s := &Stream{Input: inWriter, Output: outReader, pid: identity.PID(), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer cancel()
		waited := make(chan struct{})
		go func() { _, _ = child.Wait(); close(waited) }()
		select {
		case <-lifetime.Done():
			_ = outReader.Close()
		case <-waited:
		}
		_ = child.KillTree()
		_ = child.Kill()
		_ = inWriter.Close()
		select {
		case <-waited:
		case <-time.After(terminationWait):
		}
		_ = child.Close()
	}()
	return s, nil
}
