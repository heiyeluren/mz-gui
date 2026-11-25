package processrun

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

const (
	defaultOutputLimit = 1 << 20
	maxOutputLimit     = 16 << 20
)

type outputPump struct {
	readers []*os.File
	writers []*os.File
	wg      sync.WaitGroup
	mu      sync.Mutex
	err     error
	output  *boundedOutput
	stdout  *boundedOutput
	stderr  *boundedOutput
}

func prepareOutput(spec Spec) (Spec, *outputPump, error) {
	limit := spec.OutputLimit
	if limit == 0 {
		limit = defaultOutputLimit
	}
	if limit < 1 || limit > maxOutputLimit {
		return Spec{}, &outputPump{}, ErrInvalidSpec
	}
	pump := &outputPump{output: &boundedOutput{limit: limit}}
	if spec.SeparateOutput {
		pump.stdout = &boundedOutput{limit: limit}
		pump.stderr = &boundedOutput{limit: limit}
	}
	prepared := spec
	stdout, err := pump.add(pump.stdout)
	if err != nil {
		pump.abort()
		pump.wg.Wait()
		return Spec{}, pump, err
	}
	prepared.stdout = stdout
	stderr, err := pump.add(pump.stderr)
	if err != nil {
		pump.abort()
		pump.wg.Wait()
		return Spec{}, pump, err
	}
	prepared.stderr = stderr
	return prepared, pump, nil
}

func (pump *outputPump) add(separate *boundedOutput) (io.Writer, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	pump.readers = append(pump.readers, reader)
	pump.writers = append(pump.writers, writer)
	pump.wg.Add(1)
	var destination io.Writer = pump.output
	if separate != nil {
		destination = io.MultiWriter(pump.output, separate)
	}
	go func() {
		defer pump.wg.Done()
		_, copyErr := io.Copy(destination, reader)
		if copyErr != nil {
			pump.mu.Lock()
			pump.err = errors.Join(pump.err, copyErr)
			pump.mu.Unlock()
		}
	}()
	return writer, nil
}

func (pump *outputPump) childStarted() {
	for _, writer := range pump.writers {
		_ = writer.Close()
	}
}

func (pump *outputPump) wait() error {
	done := make(chan struct{})
	go func() {
		pump.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(terminationWait)
	defer timer.Stop()
	select {
	case <-done:
		pump.closeReaders()
		return pump.copyError()
	case <-timer.C:
		pump.closeReaders()
		<-done
		return ErrOutputDrainTimeout
	}
}

func (pump *outputPump) abortAndWait() error {
	pump.abort()
	pump.wg.Wait()
	return ErrOutputDrainTimeout
}

func (pump *outputPump) abort() {
	pump.childStarted()
	pump.closeReaders()
}

func (pump *outputPump) closeReaders() {
	for _, reader := range pump.readers {
		_ = reader.Close()
	}
}

func (pump *outputPump) copyError() error {
	pump.mu.Lock()
	defer pump.mu.Unlock()
	return pump.err
}

func (pump *outputPump) result() ([]byte, bool) {
	if pump.output == nil {
		return nil, false
	}
	return pump.output.result()
}

func (pump *outputPump) separateResult() ([]byte, []byte) {
	if pump.stdout == nil || pump.stderr == nil {
		return nil, nil
	}
	stdout, _ := pump.stdout.result()
	stderr, _ := pump.stderr.result()
	// 非 nil 空切片明确表示已分离捕获，调用方不能回退为合并输出。
	return append([]byte{}, stdout...), append([]byte{}, stderr...)
}

type boundedOutput struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func (output *boundedOutput) Write(value []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	remaining := output.limit - len(output.data)
	if remaining > 0 {
		output.data = append(output.data, value[:min(len(value), remaining)]...)
	}
	if len(value) > remaining {
		output.truncated = true
	}
	return len(value), nil
}

func (output *boundedOutput) result() ([]byte, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return append([]byte(nil), output.data...), output.truncated
}
