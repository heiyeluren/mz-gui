package testkit

import "sync"

type FakeProcess struct {
	mu       sync.Mutex
	pid      int
	running  bool
	exitCode int
	output   []string
}

func NewFakeProcess(pid int) *FakeProcess {
	return &FakeProcess{pid: pid, running: true}
}

func (process *FakeProcess) PID() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.pid
}

func (process *FakeProcess) Running() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.running
}

func (process *FakeProcess) AppendOutput(line string) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.output = append(process.output, line)
}

func (process *FakeProcess) Output() []string {
	process.mu.Lock()
	defer process.mu.Unlock()
	return append([]string(nil), process.output...)
}

func (process *FakeProcess) Exit(code int) {
	process.mu.Lock()
	defer process.mu.Unlock()
	process.running = false
	process.exitCode = code
}

func (process *FakeProcess) ExitCode() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.exitCode
}
