// Package xterm 提供独立终端状态进程；进程只解析输出，不拥有 PTY，也不写回用户输入。
package xterm

import (
	"bufio"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed assets/headless.js
var headless string

//go:embed assets/serialize.js
var serializer string

//go:embed assets/state.js
var stateAdapter string

//go:embed assets/worker.js
var worker string

var ErrUnavailable = errors.New("终端状态恢复暂不可用")

const maxMessageBytes = 3 << 20
const checkpointInterval = 256 << 10
const loader = `const readline=require('node:readline');const channel=readline.createInterface({input:process.stdin,crlfDelay:Infinity});channel.once('line',line=>{try{if(line.length>2000000)throw Error();Function('channel',JSON.parse(line))(channel)}catch{process.exit(70)}});`

type operation struct {
	Colors map[string]string `json:"colors,omitempty"`
	ID     int               `json:"id"`
	Op     string            `json:"op"`
	Data   string            `json:"data,omitempty"`
	Cols   int               `json:"cols,omitempty"`
	Rows   int               `json:"rows,omitempty"`
	State  json.RawMessage   `json:"state,omitempty"`
}
type response struct {
	Replies string          `json:"replies"`
	ID      int             `json:"id"`
	OK      bool            `json:"ok"`
	Error   string          `json:"error"`
	State   json.RawMessage `json:"state"`
}
type Engine struct {
	defaultColors    map[string]string
	stopping         atomic.Bool
	process          atomic.Pointer[os.Process]
	replies          []byte
	recoveredReplies string
	frozen           bool
	mu               sync.Mutex
	node             string
	cols, rows       int
	cmd              *exec.Cmd
	input            io.WriteCloser
	output           *bufio.Reader
	seq              int
	checkpoint       json.RawMessage
	journal          []operation
	journalBytes     int
	closed, failed   bool
}

func New(node string, cols, rows int) (*Engine, error) {
	if node == "" || cols < 2 || cols > 500 || rows < 2 || rows > 200 {
		return nil, ErrUnavailable
	}
	e := &Engine{node: node, cols: cols, rows: rows}
	if err := e.start(); err != nil {
		e.stop()
		return nil, ErrUnavailable
	}
	return e, nil
}
func (e *Engine) start() error {
	if e.stopping.Load() {
		return ErrUnavailable
	}
	cmd := exec.Command(e.node, "--max-old-space-size=96", "-e", loader)
	configureStateProcess(cmd)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "NODE_") && !strings.HasPrefix(value, "DYLD_") && !strings.HasPrefix(value, "LD_PRELOAD=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return err
	}
	e.cmd, e.input, e.output = cmd, input, bufio.NewReaderSize(output, 64<<10)
	e.process.Store(cmd.Process)
	if e.stopping.Load() {
		_ = cmd.Process.Kill()
		return ErrUnavailable
	}
	source := "const {Terminal}=(()=>{let module={exports:{}};let exports=module.exports;\n" + headless + "\n;return module.exports})();\nconst {SerializeAddon}=(()=>{let module={exports:{}};let exports=module.exports;\n" + serializer + "\n;return module.exports})();\n" + strings.Replace(stateAdapter, "export const terminalState", "const terminalState", 1) + "\n" + worker
	timer := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	if err = json.NewEncoder(input).Encode(source); err != nil {
		return err
	}
	r, err := e.read()
	if err != nil || r.ID != 0 || !r.OK {
		return ErrUnavailable
	}
	_, err = e.exchange(operation{Op: "init", Cols: e.cols, Rows: e.rows, Colors: e.defaultColors})
	return err
}
func (e *Engine) read() (response, error) {
	var data []byte
	for {
		part, prefix, err := e.output.ReadLine()
		if err != nil {
			return response{}, err
		}
		if len(data)+len(part) > maxMessageBytes {
			return response{}, ErrUnavailable
		}
		data = append(data, part...)
		if !prefix {
			break
		}
	}
	var r response
	err := json.Unmarshal(data, &r)
	return r, err
}
func (e *Engine) exchange(op operation) (response, error) {
	if e.cmd == nil {
		return response{}, ErrUnavailable
	}
	e.seq++
	op.ID = e.seq
	cmd := e.cmd
	timer := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()
	if err := json.NewEncoder(e.input).Encode(op); err != nil {
		return response{}, ErrUnavailable
	}
	r, err := e.read()
	if err != nil || r.ID != op.ID || r.Error != "" || (op.Op == "snapshot" && (len(r.State) == 0 || string(r.State) == "null")) || (op.Op != "snapshot" && !r.OK) {
		return response{}, ErrUnavailable
	}
	return r, nil
}
func (e *Engine) stop() {
	if e.input != nil {
		_ = e.input.Close()
	}
	if e.cmd != nil {
		e.process.CompareAndSwap(e.cmd.Process, nil)
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
	e.cmd = nil
	e.input = nil
	e.output = nil
}
func (e *Engine) recover() error {
	e.stop()
	if err := e.start(); err != nil {
		return err
	}
	if len(e.checkpoint) > 0 {
		if _, err := e.exchange(operation{Op: "restore", State: e.checkpoint}); err != nil {
			return err
		}
	}
	e.recoveredReplies = ""
	for _, op := range e.journal {
		r, err := e.exchange(op)
		if err != nil {
			return err
		}
		e.recoveredReplies = r.Replies
	}
	return nil
}
func (e *Engine) apply(op operation) error {
	e.journal = append(e.journal, op)
	e.journalBytes += len(op.Data) + 32
	r, err := e.exchange(op)
	if err != nil {
		if err = e.recover(); err != nil {
			e.failed = true
			e.stop()
			return ErrUnavailable
		}
		r.Replies = e.recoveredReplies
	}
	if len(e.replies)+len(r.Replies) > 256<<10 {
		e.failed = true
		e.stop()
		return ErrUnavailable
	}
	e.replies = append(e.replies, r.Replies...)
	if e.journalBytes >= checkpointInterval {
		return e.save()
	}
	return nil
}
func (e *Engine) save() error {
	r, err := e.exchange(operation{Op: "snapshot"})
	if err != nil {
		if err = e.recover(); err == nil {
			r, err = e.exchange(operation{Op: "snapshot"})
		}
	}
	if err != nil {
		e.failed = true
		e.stop()
		return ErrUnavailable
	}
	e.checkpoint = append(e.checkpoint[:0], r.State...)
	e.journal = nil
	e.journalBytes = 0
	return nil
}
func (e *Engine) Write(data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.failed || e.frozen {
		return ErrUnavailable
	}
	if len(data) == 0 {
		return nil
	}
	return e.apply(operation{Op: "write", Data: string(data)})
}

// SetDefaultColors 在 PTY 启动前绑定会话主题，重置及状态进程恢复后继续使用。
func (e *Engine) SetDefaultColors(foreground, background, cursor string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.failed || e.frozen {
		return ErrUnavailable
	}
	colors := make(map[string]string)
	for key, value := range map[string]string{"foreground": foreground, "background": background, "cursor": cursor} {
		if value == "" {
			continue
		}
		if len(value) != 7 || value[0] != '#' {
			return ErrUnavailable
		}
		if _, err := hex.DecodeString(value[1:]); err != nil {
			return ErrUnavailable
		}
		colors[key] = value
	}
	e.defaultColors = colors
	return e.apply(operation{Op: "colors", Colors: colors})
}
func (e *Engine) Resize(cols, rows int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.failed || e.frozen {
		return ErrUnavailable
	}
	if cols < 2 || cols > 500 || rows < 2 || rows > 200 {
		return ErrUnavailable
	}
	err := e.apply(operation{Op: "resize", Cols: cols, Rows: rows})
	if err == nil {
		e.cols, e.rows = cols, rows
	}
	return err
}
func (e *Engine) Serialize() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.frozen && !e.failed && !e.closed {
		return string(e.checkpoint), nil
	}
	if e.closed || e.failed {
		return "", ErrUnavailable
	}
	if err := e.save(); err != nil {
		return "", err
	}
	return string(e.checkpoint), nil
}
func (e *Engine) Restore(state string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.failed {
		return ErrUnavailable
	}
	_, err := e.exchange(operation{Op: "restore", State: json.RawMessage(state)})
	if err == nil {
		e.checkpoint = []byte(state)
		e.journal = nil
		e.journalBytes = 0
	}
	return err
}
func (e *Engine) Close() {
	e.stopping.Store(true)
	if process := e.process.Swap(nil); process != nil {
		_ = process.Kill()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	e.stop()
	e.checkpoint = nil
	e.journal = nil
}

func (e *Engine) Freeze() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.failed {
		return ErrUnavailable
	}
	if e.frozen {
		return nil
	}
	err := e.save()
	e.frozen = true
	e.stop()
	return err
}

func (e *Engine) TakeReplies() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := e.replies
	e.replies = nil
	return result
}

// ProcessID 仅用于诊断本实例的辅助进程；冻结或关闭后返回 0。
func (e *Engine) ProcessID() int {
	if process := e.process.Load(); process != nil {
		return process.Pid
	}
	return 0
}
