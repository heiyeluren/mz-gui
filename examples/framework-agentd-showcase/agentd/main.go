package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
)

func main() {
	defaultRunDir := ""
	cache, err := os.UserCacheDir()
	if err == nil {
		defaultRunDir = filepath.Join(cache, "mzgui-agentd-showcase", "run")
	}
	runDir := flag.String("run-dir", defaultRunDir, "agentd 运行与 SQLite 数据目录")
	flag.Parse()
	if *runDir == "" {
		*runDir = os.Getenv("MZGUI_AGENTD_RUN_DIR")
	}
	if *runDir == "" {
		log.Fatal("请通过 -run-dir 或 MZGUI_AGENTD_RUN_DIR 指定绝对运行目录")
	}
	absolute, err := filepath.Abs(*runDir)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	application, err := newApplication(ctx, local.Endpoint{Name: "showcase-agentd", RunDir: absolute})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := application.Close(closeContext); err != nil {
			log.Printf("关闭 agentd: %v", err)
		}
	}()
	log.Printf("agentd 已就绪: %s", absolute)
	if err := application.server.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
