package taskexample

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	"github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

type Store struct {
	Database   *sql.DB
	Repository taskengine.Repository
	Engine     *taskengine.Engine
}

func Open(ctx context.Context, runDir string) (*Store, error) {
	database, err := sqlite.Open(ctx, sqlite.Options{
		Path:    filepath.Join(runDir, "showcase.db"),
		Bundles: []sqlite.MigrationBundle{taskmigrations.Bundle()},
	})
	if err != nil {
		return nil, err
	}
	repository := taskengine.NewSQLiteRepository(database, nil)
	registry := taskengine.NewRegistry()
	if err := registry.Register("example.long_task", taskengine.Registration{
		Handler: taskengine.HandlerFunc(func(context.Context, taskengine.Task, taskengine.OutputWriter) taskengine.ExecutionResult {
			time.Sleep(300 * time.Millisecond)
			return taskengine.ExecutionResult{Summary: []byte(`{"message":"completed"}`)}
		}),
		Resumable: true,
		Timeout:   3 * time.Second,
	}); err != nil {
		_ = database.Close()
		return nil, err
	}
	engine, err := taskengine.NewEngine(repository, registry, taskengine.EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 10 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 16, WorkerPrefix: "showcase-agent",
	})
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := engine.Start(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{Database: database, Repository: repository, Engine: engine}, nil
}

func (store *Store) Close(ctx context.Context) error {
	if store == nil {
		return nil
	}
	if store.Engine != nil {
		_ = store.Engine.Close(ctx)
	}
	if store.Database != nil {
		return store.Database.Close()
	}
	return nil
}
