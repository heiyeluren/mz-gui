package configmigration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/storage/configfile"
)

func TestRunnerAppliesContinuousStepsAndCreatesBackup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.toml")
	backup := filepath.Join(root, "backups", "before.toml")
	store := configfile.New(configfile.Options{
		Path: path, MaxBytes: 1024, Initial: []byte("schema_version=1\nvalue=legacy\n"),
		BackupPath: func(string, string) string { return backup },
	})
	runner, err := New(Options{
		CurrentVersion: 3,
		DetectVersion:  detectFixtureVersion,
		ValidateCurrent: func(data []byte) error {
			if !strings.Contains(string(data), "schema_version=3") || !strings.Contains(string(data), "migrated=2") {
				return errors.New("目标文档未完整迁移")
			}
			return nil
		},
		Steps: []Step{
			{From: 1, To: 2, Migrate: replaceFixtureVersion},
			{From: 2, To: 3, Migrate: func(_ context.Context, data []byte) ([]byte, error) {
				return []byte(strings.Replace(string(data), "schema_version=2", "schema_version=3", 1) + "migrated=2\n"), nil
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, migrated, err := runner.Upgrade(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || !strings.Contains(string(snapshot.Data), "schema_version=3") {
		t.Fatalf("迁移结果错误: migrated=%v data=%q", migrated, snapshot.Data)
	}
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != "schema_version=1\nvalue=legacy\n" {
		t.Fatalf("迁移备份错误: data=%q err=%v", data, err)
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("迁移备份权限错误: info=%v err=%v", info, err)
	}

	unchanged, migrated, err := runner.Upgrade(context.Background(), store)
	if err != nil || migrated || unchanged.Revision != snapshot.Revision {
		t.Fatalf("重复迁移必须幂等: migrated=%v snapshot=%#v err=%v", migrated, unchanged, err)
	}
}

func TestRunnerRejectsFutureVersionAndMigrationFailureWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name    string
		initial string
		step    Step
	}{
		{name: "未来版本", initial: "schema_version=3\n", step: Step{From: 1, To: 2, Migrate: replaceFixtureVersion}},
		{name: "迁移失败", initial: "schema_version=1\n", step: Step{From: 1, To: 2, Migrate: func(context.Context, []byte) ([]byte, error) { return nil, errors.New("fixture failed") }}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.toml")
			store := configfile.New(configfile.Options{Path: path, MaxBytes: 1024, Initial: []byte(test.initial)})
			runner, err := New(Options{CurrentVersion: 2, DetectVersion: detectFixtureVersion, ValidateCurrent: func([]byte) error { return nil }, Steps: []Step{test.step}})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := runner.Upgrade(context.Background(), store); err == nil {
				t.Fatal("非法迁移必须失败")
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != test.initial {
				t.Fatalf("失败迁移改写了原文档: data=%q err=%v", data, readErr)
			}
		})
	}
}

func TestRunnerRejectsInvalidRegistryAndCancellation(t *testing.T) {
	options := Options{CurrentVersion: 3, DetectVersion: detectFixtureVersion, ValidateCurrent: func([]byte) error { return nil }}
	for _, steps := range [][]Step{
		nil,
		{{From: 1, To: 3, Migrate: replaceFixtureVersion}},
		{{From: 1, To: 2, Migrate: replaceFixtureVersion}, {From: 1, To: 2, Migrate: replaceFixtureVersion}},
	} {
		options.Steps = steps
		if _, err := New(options); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("非法 registry 错误 = %v", err)
		}
	}

	options.CurrentVersion = 2
	options.Steps = []Step{{From: 1, To: 2, Migrate: replaceFixtureVersion}}
	runner, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	store := configfile.New(configfile.Options{Path: filepath.Join(t.TempDir(), "app.toml"), MaxBytes: 128, Initial: []byte("schema_version=1\n")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := runner.Upgrade(ctx, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

func detectFixtureVersion(data []byte) (int, error) {
	const prefix = "schema_version="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strconv.Atoi(strings.TrimPrefix(line, prefix))
		}
	}
	return 0, errors.New("缺少版本")
}

func replaceFixtureVersion(_ context.Context, data []byte) ([]byte, error) {
	return []byte(strings.Replace(string(data), "schema_version=1", "schema_version=2", 1)), nil
}
