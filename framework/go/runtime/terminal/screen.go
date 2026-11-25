package terminal

import (
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"
)

// Screen是短期、只读的VT屏幕投影，不拥有PTY，不自动发送设备应答或用户输入。
type Screen struct {
	mu      sync.Mutex
	vt      *vt.Emulator
	drained chan struct{}
	closed  bool
}

type ScreenSnapshot struct {
	Lines            []string
	CursorX, CursorY int
}

func NewScreen(cols, rows int) (*Screen, error) {
	if !validScreenSize(cols, rows) {
		return nil, errors.New("终端屏幕尺寸无效")
	}
	emulator := vt.NewEmulator(cols, rows)
	emulator.SetScrollbackSize(1)
	screen := &Screen{vt: emulator, drained: make(chan struct{})}
	go func() { defer close(screen.drained); _, _ = io.Copy(io.Discard, emulator) }()
	return screen, nil
}

func validScreenSize(cols, rows int) bool {
	return cols >= 2 && cols <= 500 && rows >= 2 && rows <= 200
}

func (screen *Screen) Feed(data []byte, cols, rows int) error {
	if len(data) > 1<<20 || !validScreenSize(cols, rows) {
		return errors.New("终端屏幕输入超出上限")
	}
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if screen.closed {
		return io.ErrClosedPipe
	}
	if screen.vt.Width() != cols || screen.vt.Height() != rows {
		screen.vt.Resize(cols, rows)
	}
	_, err := screen.vt.Write(data)
	return err
}

func (screen *Screen) Snapshot() ScreenSnapshot {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if screen.closed {
		return ScreenSnapshot{}
	}
	cursor := screen.vt.CursorPosition()
	value := ScreenSnapshot{CursorX: cursor.X, CursorY: cursor.Y, Lines: make([]string, screen.vt.Height())}
	for y := range value.Lines {
		var line strings.Builder
		for x := 0; x < screen.vt.Width(); x++ {
			cell := screen.vt.CellAt(x, y)
			if cell == nil {
				line.WriteByte(' ')
			} else if cell.Content != "" {
				line.WriteString(cell.Content)
			} else if cell.Width > 0 {
				line.WriteByte(' ')
			}
		}
		value.Lines[y] = line.String()
	}
	return value
}

func (screen *Screen) Close() error {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if screen.closed {
		return nil
	}
	screen.closed = true
	// 先关闭应答管道并等待reader结束，再改模拟器的closed字段，避免并发读写该标志。
	err := screen.vt.InputPipe().(io.Closer).Close()
	<-screen.drained
	return errors.Join(err, screen.vt.Close())
}
