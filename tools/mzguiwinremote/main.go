package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/winremote"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法: mzguiwinremote <collect|inspect> [选项]")
		return 2
	}
	switch args[0] {
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "collect":
		return runCollect(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知子命令: %s\n", args[0])
		return 2
	}
}

func runInspect(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguiwinremote inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	from := flags.String("from", "", "windows-x64-evidence.json 路径")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*from) == "" {
		fmt.Fprintln(stderr, "必须提供 --from")
		return 2
	}
	data, err := os.ReadFile(*from)
	if err != nil {
		fmt.Fprintf(stderr, "读取证据失败: %v\n", err)
		return 1
	}
	evidence, err := winremote.Decode(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printEvidence(stdout, evidence)
	if !evidence.AllowsLocalAutomated() {
		return 3
	}
	return 0
}

func runCollect(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguiwinremote collect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	host := flags.String("host", "", "Windows 局域网 IP 或主机名")
	user := flags.String("user", "", "Windows 用户名")
	identity := flags.String("identity", "", "SSH 私钥路径")
	port := flags.Int("port", 22, "SSH 端口")
	remoteRepo := flags.String("remote-repo", "", "Windows 上的仓库绝对路径")
	remoteOutput := flags.String("remote-output", "", "Windows 证据输出目录")
	output := flags.String("output", "", "本机证据 JSON 输出路径")
	commit := flags.String("commit", "", "期望提交，默认使用本机 HEAD")
	timeout := flags.Duration("timeout", 30*time.Minute, "远端测试超时")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *host == "" || *user == "" || *remoteRepo == "" || *output == "" {
		fmt.Fprintln(stderr, "必须提供 --host --user --remote-repo --output")
		return 2
	}
	expectedCommit := strings.TrimSpace(*commit)
	if expectedCommit == "" {
		head, err := localHEAD()
		if err != nil {
			fmt.Fprintf(stderr, "读取本机 HEAD 失败: %v\n", err)
			return 1
		}
		expectedCommit = head
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	transport := SSHTransport{
		Host:     *host,
		User:     *user,
		Identity: *identity,
		Port:     *port,
	}
	evidence, err := winremote.Collect(ctx, transport, winremote.Options{
		RemoteRepo:   *remoteRepo,
		RemoteOutput: *remoteOutput,
		LocalOutput:  *output,
		Commit:       expectedCommit,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	printEvidence(stdout, evidence)
	if !evidence.AllowsLocalAutomated() {
		fmt.Fprintln(stderr, "证据未通过，不得把 windows-x64 标成本机自动验证")
		return 3
	}
	return 0
}

func printEvidence(stdout io.Writer, evidence winremote.Evidence) {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(map[string]any{
		"platform":               evidence.Platform,
		"hostname":               evidence.Hostname,
		"commit":                 evidence.Commit,
		"status":                 evidence.Status,
		"allows_local_automated": evidence.AllowsLocalAutomated(),
		"steps":                  evidence.Steps,
	})
}

func localHEAD() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type SSHTransport struct {
	Host     string
	User     string
	Identity string
	Port     int
}

func (transport SSHTransport) Run(ctx context.Context, argv []string) (winremote.CommandResult, error) {
	args := transport.baseArgs("ssh")
	args = append(args, transport.target())
	args = append(args, argv...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdout, err := cmd.Output()
	result := winremote.CommandResult{Stdout: string(stdout)}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			result.Code = exit.ExitCode()
			result.Stderr = string(exit.Stderr)
		}
		return result, fmt.Errorf("ssh 执行失败: %w", err)
	}
	return result, nil
}

func (transport SSHTransport) Pull(ctx context.Context, remotePath, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	args := transport.baseArgs("scp")
	args = append(args, transport.target()+":"+toSCPPath(remotePath), localPath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scp 拉取失败: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (transport SSHTransport) baseArgs(tool string) []string {
	port := transport.Port
	if port == 0 {
		port = 22
	}
	portFlag := "-p"
	if tool == "scp" {
		portFlag = "-P"
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		portFlag, strconv.Itoa(port),
	}
	if identity := strings.TrimSpace(transport.Identity); identity != "" {
		args = append(args, "-i", identity)
	}
	return args
}

func (transport SSHTransport) target() string {
	return transport.User + "@" + transport.Host
}

func toSCPPath(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}
