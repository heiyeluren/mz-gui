package processrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunForwardsEnvironmentInputAndOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Spec{
		Executable: executable,
		Arguments:  []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(),
			"MZ_GUI_PROCESSRUN_HELPER=echo", "MZ_GUI_PROCESSRUN_VALUE=framework-value"),
		Input: []byte("stdin-value"),
	})
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("执行结果=%#v", result)
	}
	if output := string(result.Output); !strings.Contains(output, "framework-value:stdin-value") || !strings.Contains(output, "helper-stderr") {
		t.Fatalf("输出不匹配: %q", output)
	}
}

func TestRunBoundsCapturedOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Spec{
		Executable: executable,
		Arguments:  []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(),
			"MZ_GUI_PROCESSRUN_HELPER=output"),
		OutputLimit: 64,
	})
	if result.Err != nil || len(result.Output) != 64 || !result.OutputTruncated {
		t.Fatalf("有界输出结果=%#v", result)
	}
}

func TestRunSeparatesStreamsWithoutLosingCombinedOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Spec{Executable: executable,
		Arguments:   []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(), "MZ_GUI_PROCESSRUN_HELPER=echo", "MZ_GUI_PROCESSRUN_VALUE=stdout"),
		Input:       []byte("input"), SeparateOutput: true})
	if result.Err != nil || string(result.Stdout) != "stdout:input" || string(result.Stderr) != "helper-stderr" || !strings.Contains(string(result.Output), "helper-stderr") {
		t.Fatalf("分离输出错误: %#v", result)
	}
}

func TestRunSeparateOutputKeepsTruncationLimit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Spec{Executable: executable, Arguments: []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(), "MZ_GUI_PROCESSRUN_HELPER=output"), OutputLimit: 64, SeparateOutput: true})
	if result.Err != nil || !result.OutputTruncated || len(result.Stdout) != 64 || len(result.Output) != 64 || result.Stderr == nil || len(result.Stderr) != 0 {
		t.Fatalf("分离输出未保留截断与空流: %#v", result)
	}
}

func TestRunCancelsProcessTreePromptly(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	result := Run(ctx, Spec{
		Executable: executable,
		Arguments:  []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(),
			"MZ_GUI_PROCESSRUN_HELPER=sleep"),
	})
	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("取消错误=%v", result.Err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("取消耗时=%s", elapsed)
	}
}

func TestRunRejectsInvalidSpec(t *testing.T) {
	if result := Run(nil, Spec{Executable: "tool"}); !errors.Is(result.Err, ErrInvalidSpec) {
		t.Fatalf("nil Context 结果=%#v", result)
	}
	if result := Run(context.Background(), Spec{}); !errors.Is(result.Err, ErrInvalidSpec) {
		t.Fatalf("空命令结果=%#v", result)
	}
}

func TestRunPreservesNonZeroExitCode(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), Spec{
		Executable: executable,
		Arguments:  []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(),
			"MZ_GUI_PROCESSRUN_HELPER=exit"),
	})
	if result.ExitCode != 7 || result.Err == nil {
		t.Fatalf("非零退出结果=%#v", result)
	}
}

func TestProcessRunHelper(t *testing.T) {
	switch os.Getenv("MZ_GUI_PROCESSRUN_HELPER") {
	case "echo":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintf(os.Stdout, "%s:%s", os.Getenv("MZ_GUI_PROCESSRUN_VALUE"), input)
		_, _ = fmt.Fprint(os.Stderr, "helper-stderr")
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "exit":
		os.Exit(7)
	case "output":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
		os.Exit(0)
	case "spawn-child":
		command := exec.Command(os.Args[0], "-test.run=^TestProcessRunHelper$")
		command.Env = append(os.Environ(), "MZ_GUI_PROCESSRUN_HELPER=sleep")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv("MZ_GUI_PROCESSRUN_PID_FILE"), []byte(strconv.Itoa(command.Process.Pid)), 0o600); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	}
}
