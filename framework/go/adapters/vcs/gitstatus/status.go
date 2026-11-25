package gitstatus

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxOutputBytes = 2 << 20

var (
	ErrInvalidPath    = errors.New("gitstatus: invalid path")
	ErrGitUnavailable = errors.New("gitstatus: git unavailable")
	ErrInspectFailed  = errors.New("gitstatus: inspect failed")
	ErrOutputTooLarge = errors.New("gitstatus: output too large")
	ErrMalformed      = errors.New("gitstatus: malformed output")
)

type Config struct {
	Executable string
	Timeout    time.Duration
}

type Snapshot struct {
	IsRepository bool
	Branch       string
	Detached     bool
	Ahead        int
	Behind       int
	Staged       int
	Modified     int
	Untracked    int
	Conflicted   int
	Clean        bool
	StatusCode   string
}

type Inspector struct {
	executable string
	timeout    time.Duration
}

func New(config Config) *Inspector {
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = "git"
	}
	if config.Timeout <= 0 {
		config.Timeout = 3 * time.Second
	}
	return &Inspector{executable: executable, timeout: config.Timeout}
}

func (inspector *Inspector) Inspect(ctx context.Context, root string) (Snapshot, error) {
	clean, err := validatedDirectory(root)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := exec.LookPath(inspector.executable); err != nil {
		return Snapshot{}, ErrGitUnavailable
	}
	commandCtx, cancel := context.WithTimeout(ctx, inspector.timeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, inspector.executable, "-C", clean, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var stdout limitedBuffer
	var stderr limitedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if commandCtx.Err() != nil {
		return Snapshot{}, commandCtx.Err()
	}
	if errors.Is(stdout.err, ErrOutputTooLarge) || errors.Is(stderr.err, ErrOutputTooLarge) {
		return Snapshot{}, ErrOutputTooLarge
	}
	if err != nil {
		if strings.Contains(strings.ToLower(stderr.String()), "not a git repository") {
			return Snapshot{StatusCode: "not_repository"}, nil
		}
		return Snapshot{}, ErrInspectFailed
	}
	snapshot, err := parsePorcelainV2(stdout.Bytes())
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.IsRepository = true
	snapshot.StatusCode = "ready"
	return snapshot, nil
}

func validatedDirectory(root string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(root))
	if clean == "." || !filepath.IsAbs(clean) {
		return "", ErrInvalidPath
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func parsePorcelainV2(data []byte) (Snapshot, error) {
	snapshot := Snapshot{IsRepository: true, Clean: true}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxOutputBytes)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			snapshot.Branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
			if snapshot.Branch == "(detached)" {
				snapshot.Branch = "HEAD"
				snapshot.Detached = true
			}
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(fields) != 2 {
				return Snapshot{}, ErrMalformed
			}
			var err error
			if snapshot.Ahead, err = parseSignedCount(fields[0], '+'); err != nil {
				return Snapshot{}, err
			}
			if snapshot.Behind, err = parseSignedCount(fields[1], '-'); err != nil {
				return Snapshot{}, err
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			fields := strings.Fields(line)
			if len(fields) < 2 || len(fields[1]) != 2 {
				return Snapshot{}, ErrMalformed
			}
			index, worktree := fields[1][0], fields[1][1]
			if index != '.' {
				snapshot.Staged++
			}
			if worktree != '.' {
				snapshot.Modified++
			}
			if index == 'U' || worktree == 'U' {
				snapshot.Conflicted++
			}
		case strings.HasPrefix(line, "u "):
			snapshot.Conflicted++
		case strings.HasPrefix(line, "? "):
			snapshot.Untracked++
		case line == "", strings.HasPrefix(line, "# "), strings.HasPrefix(line, "! "):
		default:
			return Snapshot{}, fmt.Errorf("%w: unknown record", ErrMalformed)
		}
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, ErrOutputTooLarge
	}
	snapshot.Clean = snapshot.Staged == 0 && snapshot.Modified == 0 && snapshot.Untracked == 0 && snapshot.Conflicted == 0
	return snapshot, nil
}

func parseSignedCount(value string, prefix byte) (int, error) {
	if len(value) < 2 || value[0] != prefix {
		return 0, ErrMalformed
	}
	parsed, err := strconv.Atoi(value[1:])
	if err != nil || parsed < 0 {
		return 0, ErrMalformed
	}
	return parsed, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	err    error
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	if buffer.buffer.Len()+len(value) > maxOutputBytes {
		buffer.err = ErrOutputTooLarge
		return 0, buffer.err
	}
	return buffer.buffer.Write(value)
}

func (buffer *limitedBuffer) Bytes() []byte  { return buffer.buffer.Bytes() }
func (buffer *limitedBuffer) String() string { return buffer.buffer.String() }
