// Package configmigration 提供产品无关的配置版本迁移编排。
package configmigration

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/storage/configfile"
)

var (
	ErrInvalidOptions     = errors.New("配置迁移参数无效")
	ErrUnsupportedVersion = errors.New("配置版本不受支持")
)

type DetectVersionFunc func([]byte) (int, error)
type ValidateCurrentFunc func([]byte) error
type MigrateFunc func(context.Context, []byte) ([]byte, error)

type Step struct {
	From    int
	To      int
	Migrate MigrateFunc
}

type Options struct {
	CurrentVersion  int
	DetectVersion   DetectVersionFunc
	ValidateCurrent ValidateCurrentFunc
	Steps           []Step
}

type Runner struct {
	current  int
	detect   DetectVersionFunc
	validate ValidateCurrentFunc
	steps    map[int]Step
	mu       sync.Mutex
}

func New(options Options) (*Runner, error) {
	if options.CurrentVersion < 1 || options.DetectVersion == nil || options.ValidateCurrent == nil || len(options.Steps) == 0 {
		return nil, ErrInvalidOptions
	}
	steps := make(map[int]Step, len(options.Steps))
	for _, step := range options.Steps {
		if step.From < 1 || step.To != step.From+1 || step.To > options.CurrentVersion || step.Migrate == nil {
			return nil, ErrInvalidOptions
		}
		if _, exists := steps[step.From]; exists {
			return nil, ErrInvalidOptions
		}
		steps[step.From] = step
	}
	versions := make([]int, 0, len(steps))
	for version := range steps {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	if versions[0] != 1 || versions[len(versions)-1]+1 != options.CurrentVersion {
		return nil, ErrInvalidOptions
	}
	for index, version := range versions {
		if version != index+1 {
			return nil, ErrInvalidOptions
		}
	}
	return &Runner{current: options.CurrentVersion, detect: options.DetectVersion, validate: options.ValidateCurrent, steps: steps}, nil
}

func (runner *Runner) Upgrade(ctx context.Context, store *configfile.Store) (configfile.Snapshot, bool, error) {
	if ctx == nil || runner == nil || store == nil {
		return configfile.Snapshot{}, false, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return configfile.Snapshot{}, false, err
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()

	snapshot, err := store.LoadOrCreate()
	if err != nil {
		return configfile.Snapshot{}, false, err
	}
	version, err := runner.detect(snapshot.Data)
	if err != nil || version < 1 || version > runner.current {
		return configfile.Snapshot{}, false, errors.Join(ErrUnsupportedVersion, err)
	}
	if version == runner.current {
		if err := runner.validate(snapshot.Data); err != nil {
			return configfile.Snapshot{}, false, fmt.Errorf("校验当前配置: %w", err)
		}
		return snapshot, false, nil
	}

	candidate := append([]byte(nil), snapshot.Data...)
	for version < runner.current {
		if err := ctx.Err(); err != nil {
			return configfile.Snapshot{}, false, err
		}
		step, exists := runner.steps[version]
		if !exists {
			return configfile.Snapshot{}, false, ErrUnsupportedVersion
		}
		candidate, err = step.Migrate(ctx, candidate)
		if err != nil {
			return configfile.Snapshot{}, false, fmt.Errorf("迁移配置 %d -> %d: %w", step.From, step.To, err)
		}
		version, err = runner.detect(candidate)
		if err != nil || version != step.To {
			return configfile.Snapshot{}, false, ErrUnsupportedVersion
		}
	}
	if err := runner.validate(candidate); err != nil {
		return configfile.Snapshot{}, false, fmt.Errorf("校验迁移配置: %w", err)
	}
	updated, err := store.Update(snapshot.Revision, candidate)
	if err != nil {
		return configfile.Snapshot{}, false, err
	}
	return updated, true, nil
}
