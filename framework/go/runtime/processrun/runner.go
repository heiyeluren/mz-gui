// Package processrun 提供一次性子进程的跨平台进程树执行能力。
package processrun

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var (
	ErrInvalidSpec        = errors.New("进程执行参数无效")
	ErrTerminationTimeout = errors.New("进程树终止等待超时")
	ErrOutputDrainTimeout = errors.New("进程输出回收等待超时")
)

const terminationWait = 2 * time.Second

type Spec struct {
	Executable  string
	Arguments   []string
	Environment []string
	Directory   string
	Input       []byte
	OutputLimit int
	// SeparateOutput 保留各自有界的输出，同时兼容原有合并输出。
	SeparateOutput bool
	stdout         io.Writer
	stderr         io.Writer
	stdin          io.Reader
}

type Result struct {
	ExitCode        int
	Output          []byte
	Stdout          []byte
	Stderr          []byte
	OutputTruncated bool
	Err             error
}

type process interface {
	Wait() (int, error)
	KillTree() error
	Kill() error
	Close() error
}

func Run(ctx context.Context, spec Spec) Result {
	if ctx == nil || strings.TrimSpace(spec.Executable) == "" {
		return Result{ExitCode: -1, Err: ErrInvalidSpec}
	}
	if err := ctx.Err(); err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	launchSpec, output, err := prepareOutput(spec)
	if err != nil {
		return Result{ExitCode: -1, Err: err}
	}
	child, err := launch(ctx, launchSpec)
	output.childStarted()
	if err != nil {
		return Result{ExitCode: -1, Err: errors.Join(err, output.wait())}
	}
	type waitResult struct {
		exitCode int
		err      error
	}
	waited := make(chan waitResult, 1)
	go func() {
		exitCode, waitErr := child.Wait()
		waited <- waitResult{exitCode: exitCode, err: waitErr}
	}()
	select {
	case result := <-waited:
		cleanupErr := child.KillTree()
		closeErr := child.Close()
		outputErr := output.wait()
		captured, truncated := output.result()
		stdout, stderr := output.separateResult()
		return Result{ExitCode: result.exitCode, Output: captured, Stdout: stdout, Stderr: stderr, OutputTruncated: truncated, Err: errors.Join(result.err, cleanupErr, closeErr, outputErr)}
	case <-ctx.Done():
		treeErr := child.KillTree()
		directErr := child.Kill()
		timer := time.NewTimer(terminationWait)
		defer timer.Stop()
		select {
		case result := <-waited:
			closeErr := child.Close()
			outputErr := output.wait()
			captured, truncated := output.result()
			stdout, stderr := output.separateResult()
			return Result{ExitCode: result.exitCode, Output: captured, Stdout: stdout, Stderr: stderr, OutputTruncated: truncated, Err: errors.Join(ctx.Err(), treeErr, directErr, result.err, closeErr, outputErr)}
		case <-timer.C:
			closeErr := child.Close()
			outputErr := output.abortAndWait()
			captured, truncated := output.result()
			stdout, stderr := output.separateResult()
			return Result{ExitCode: -1, Output: captured, Stdout: stdout, Stderr: stderr, OutputTruncated: truncated, Err: errors.Join(ctx.Err(), treeErr, directErr, ErrTerminationTimeout, closeErr, outputErr)}
		}
	}
}
