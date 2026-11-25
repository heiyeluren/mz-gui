package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/remotedebug"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "serve" {
		serveArgs := args
		if len(args) > 0 && args[0] == "serve" {
			serveArgs = args[1:]
		}
		return runServe(serveArgs, stdout, stderr)
	}
	switch args[0] {
	case "run":
		return runRemote(args[1:], stdout, stderr)
	case "get":
		return runGet(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "用法: mzgui-remote-debug [serve] | run --host IP 命令 | get --host IP 路径\n")
		return 2
	}
}

func runServe(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzgui-remote-debug serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", remotedebug.DefaultPort, "监听端口")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	remotedebug.TryOpenFirewall(*port)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	server := &remotedebug.Server{Addr: addr, Password: remotedebug.DefaultPassword}
	listenAddr, closeServer, err := server.Start(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "启动失败: %v\n", err)
		return 1
	}
	defer closeServer()
	fmt.Fprintf(stdout, "mzgui-remote-debug 已启动 %s  密码已内置，对端只需 IP\n", listenAddr)
	printLocalIPs(stdout, *port)
	<-ctx.Done()
	return 0
}

func runRemote(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzgui-remote-debug run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	host := flags.String("host", "", "对端 IP")
	port := flags.Int("port", remotedebug.DefaultPort, "对端端口")
	timeout := flags.Duration("timeout", 30*time.Minute, "命令超时")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	command := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if *host == "" || command == "" {
		fmt.Fprintln(stderr, "必须提供 --host 和命令")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+10*time.Second)
	defer cancel()
	client, err := remotedebug.Dial(ctx, net.JoinHostPort(*host, fmt.Sprintf("%d", *port)), remotedebug.DefaultPassword)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer client.Close()
	result, err := client.Run(ctx, command, *timeout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, result.Stdout)
	fmt.Fprint(stderr, result.Stderr)
	return result.ExitCode
}

func runGet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzgui-remote-debug get", flag.ContinueOnError)
	flags.SetOutput(stderr)
	host := flags.String("host", "", "对端 IP")
	port := flags.Int("port", remotedebug.DefaultPort, "对端端口")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	path := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if *host == "" || path == "" {
		fmt.Fprintln(stderr, "必须提供 --host 和文件路径")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := remotedebug.Dial(ctx, net.JoinHostPort(*host, fmt.Sprintf("%d", *port)), remotedebug.DefaultPassword)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer client.Close()
	data, err := client.Get(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = stdout.Write(data)
	return 0
}

func printLocalIPs(stdout io.Writer, port int) {
	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, addr := range ifaces {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP == nil || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
			continue
		}
		fmt.Fprintf(stdout, "本机地址 %s:%d\n", ipnet.IP.String(), port)
	}
}
