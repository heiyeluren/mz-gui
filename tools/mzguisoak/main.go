package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	"github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

type soakReport struct {
	Duration         time.Duration `json:"duration"`
	TasksCreated     int           `json:"tasks_created"`
	OpenConnections  int           `json:"open_connections"`
	GoroutineDelta   int           `json:"goroutine_delta"`
	HeapAllocBytes   uint64        `json:"heap_alloc_bytes"`
	CompletedBatches int           `json:"completed_batches"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mzguisoak", flag.ContinueOnError)
	flags.SetOutput(stderr)
	duration := flags.Duration("duration", 30*time.Minute, "soak 持续时间")
	reportPath := flags.String("report", "", "报告 JSON 输出路径")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *duration <= 0 {
		fmt.Fprintln(stderr, "soak 时长必须大于零")
		return 2
	}
	report, err := execute(*duration)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *reportPath != "" {
		if err := os.MkdirAll(filepath.Dir(*reportPath), 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.WriteFile(*reportPath, append(data, '\n'), 0o600); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func execute(duration time.Duration) (soakReport, error) {
	initialGoroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	databaseFile, err := os.CreateTemp("", "mzgui-soak-*.db")
	if err != nil {
		return soakReport{}, err
	}
	databasePath := databaseFile.Name()
	_ = databaseFile.Close()
	defer os.Remove(databasePath)
	database, err := sqlite.Open(ctx, sqlite.Options{
		Path:    databasePath,
		Bundles: []sqlite.MigrationBundle{taskmigrations.Bundle()},
	})
	if err != nil {
		return soakReport{}, err
	}
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = database.Close()
		return soakReport{}, err
	}
	repository := taskengine.NewSQLiteRepository(database, nil)
	registry := taskengine.NewRegistry()
	if err := registry.Register("soak.noop", taskengine.Registration{
		Handler: taskengine.HandlerFunc(func(context.Context, taskengine.Task, taskengine.OutputWriter) taskengine.ExecutionResult {
			return taskengine.ExecutionResult{Summary: []byte(`{"message":"completed"}`)}
		}),
		Timeout: time.Second,
	}); err != nil {
		_ = database.Close()
		return soakReport{}, err
	}
	engine, err := taskengine.NewEngine(repository, registry, taskengine.EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 10 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 16, WorkerPrefix: "soak",
	})
	if err != nil {
		_ = database.Close()
		return soakReport{}, err
	}
	if err := engine.Start(ctx); err != nil {
		_ = database.Close()
		return soakReport{}, err
	}
	created := 0
	batches := 0
	for {
		done := false
		for index := 0; index < 100; index++ {
			_, _, err := repository.Create(ctx, taskengine.CreateRequest{
				Type: "soak.noop", Priority: 50, PayloadVersion: 1, Payload: []byte(`{}`),
				IdempotencyKey: fmt.Sprintf("soak-%d-%d", batches, index), WorkspaceID: "soak", MaxAttempts: 1, Now: time.Now().UTC(),
			})
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					done = true
					break
				}
				_ = engine.Close(context.Background())
				_ = database.Close()
				return soakReport{}, err
			}
			created++
		}
		if done {
			closeContext, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer closeCancel()
			if err := engine.Close(closeContext); err != nil {
				_ = database.Close()
				return soakReport{}, err
			}
			if err := database.Close(); err != nil {
				return soakReport{}, err
			}
			runtime.GC()
			return soakReport{
				Duration: duration, TasksCreated: created, OpenConnections: 0,
				GoroutineDelta: runtime.NumGoroutine() - initialGoroutines,
				HeapAllocBytes: currentHeapAlloc(), CompletedBatches: batches,
			}, nil
		}
		batches++
		select {
		case <-ctx.Done():
			closeContext, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer closeCancel()
			if err := engine.Close(closeContext); err != nil {
				_ = database.Close()
				return soakReport{}, err
			}
			if err := database.Close(); err != nil {
				return soakReport{}, err
			}
			runtime.GC()
			return soakReport{
				Duration: duration, TasksCreated: created, OpenConnections: 0,
				GoroutineDelta: runtime.NumGoroutine() - initialGoroutines,
				HeapAllocBytes: currentHeapAlloc(), CompletedBatches: batches,
			}, nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func currentHeapAlloc() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}
