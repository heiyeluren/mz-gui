package terminal

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

const (
	StateRunning     = "running"
	StateDetached    = "detached"
	StateClosing     = "closing"
	StateClosed      = "closed"
	StateExited      = "exited"
	StateInterrupted = "interrupted"

	MaxInputBytes = 256 << 10
	minCols       = 20
	maxCols       = 500
	minRows       = 5
	maxRows       = 200
)

var (
	ErrInvalidArgument      = errors.New("终端请求无效")
	ErrPathNotGranted       = errors.New("终端工作目录未授权")
	ErrSessionNotFound      = errors.New("终端会话不存在")
	ErrSessionLimit         = errors.New("终端会话数已达上限")
	ErrSessionRetentionBusy = fmt.Errorf("%w：已结束终端仍在最终输出保留窗口内，请稍后重试", ErrSessionLimit)
	ErrSessionClosed        = errors.New("终端会话已结束")
	ErrResourceUnavailable  = errors.New("终端资源组不可用")
)

var terminalScopePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidScope 校验跨应用共用的终端会话作用域格式。
func ValidScope(scope string) bool {
	return terminalScopePattern.MatchString(scope)
}

type SessionSpec struct {
	// DefaultColors 在程序启动前配置设备颜色应答，与调用方画布主题保持一致。
	DefaultColors DefaultColors
	// Observation 只携带调用方提供的非敏感关联标签，不参与终端执行。
	Observation  map[string]string
	CWD          string
	ShellProfile string
	Scope        string
	Title        string
	Program      *ProgramSpec
	// PrevalidatedCWD 表示调用方已通过更上层的 workspace grant 或等价能力校验。
	PrevalidatedCWD bool
	Cols            int
	Rows            int
}

// ProgramSpec 允许应用在已经完成白名单校验后直接启动程序，绕过系统 shell。
type ProgramSpec struct {
	// Multiplexer 显式选用外部持久终端；后台关闭只断开连接，用户关闭仍终止会话。
	Multiplexer *MultiplexerSpec
	// LiteralLastArgument 仅允许最后一个直接传给程序的文本参数保留换行与制表符。
	LiteralLastArgument bool
	// Stdin 非nil时把固定输入作为独立流交给程序，并在结束后发送真实EOF；nil保留交互PTY。
	Stdin       []byte
	Executable  string
	Arguments   []string
	Environment map[string]string
}

type SessionInfo struct {
	SessionID    string
	CWD          string
	ShellProfile string
	Scope        string
	Title        string
	State        string
	PID          int
	Cols         int
	Rows         int
	Cursor       uint64
	// InputRevision 在用户/任务输入尝试前递增；纯设备应答不计数，与输出 Cursor 独立。
	// 输入失败后也不回退。
	InputRevision uint64
	// 仅供运行时核对对话框导航；不记录输入内容，也不改变普通输入版本。
	LastNonNavigationInputRevision uint64 `json:"-"`
	// 最近一次包含回车/换行的输入尝试，用于消费方区分选择导航与提交；不记录正文。
	LastSubmitInputRevision uint64 `json:"-"`
	// 与提交版本一同记录的时间，用于校验先产生的外部状态是否已被回答消费。
	LastSubmitAt time.Time `json:"-"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ExitedAt     *time.Time
	ExitCode     *int
}

type Snapshot struct {
	ScreenCursor  uint64
	ScreenManaged bool
	ScreenState   string
	Session       SessionInfo
	Frames        []OutputFrame
	OldestCursor  uint64
	NextCursor    uint64
	Truncated     bool
	HasMore       bool
}

type ManagerOptions struct {
	Observer         Observer
	ViewStateFactory ViewStateFactory
	Launcher         Launcher
	AllowedRoots     []string
	// MaxSessions 为 0 时不限活跃会话数量；正数用于显式配额。
	MaxSessions   int
	BufferBytes   int
	SnapshotBytes int
	// 历史缓存目标与终态保留窗口；0 分别使用 16 个和 2 分钟，不限模式下不作为创建门禁。
	MaxRetainedSessions     int
	CompletedRetentionGrace time.Duration
	CloseGrace              time.Duration
	IDGenerator             func() (string, error)
	Now                     func() time.Time
	// ResourcePool 可选的会话全生命周期租约池；不限会话的消费者应留空。
	ResourcePool *resourcepool.Pool
}

// Device 是可读写并支持调整尺寸的跨平台终端设备。
type Device interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// Process 管理终端子进程及其完整进程树生命周期。
type Process interface {
	PID() int
	Wait() (int, error)
	Interrupt() error
	KillTree() error
}

type LaunchRequest struct {
	Multiplexer *MultiplexerSpec
	Stdin       []byte
	CWD         string
	Executable  string
	Arguments   []string
	Environment map[string]string
	Cols        int
	Rows        int
}

// Launcher 是平台终端实现和测试 fake 的稳定扩展点。
type Launcher interface {
	Launch(context.Context, LaunchRequest) (Device, Process, error)
}

type Manager struct {
	observer                Observer
	observationWG           sync.WaitGroup
	viewStateFactory        ViewStateFactory
	ctx                     context.Context
	cancel                  context.CancelFunc
	launcher                Launcher
	allowedRoots            []string
	maxSessions             int
	bufferBytes             int
	snapshotBytes           int
	maxRetainedSessions     int
	completedRetentionGrace time.Duration
	closeGrace              time.Duration
	newID                   func() (string, error)
	now                     func() time.Time
	resourcePool            *resourcepool.Pool

	mu       sync.RWMutex
	sessions map[string]*managedSession
	pending  int
	closed   bool
}

type managedSession struct {
	observationTags map[string]string
	outputMu        sync.Mutex
	viewState       ViewState
	viewErr         error
	mu              sync.RWMutex
	ioGate          chan struct{}
	// 输入状态由 mu 保护；后台设备写仍存活时，保持 ioGate 的所有权。
	writeUncertain bool
	writeChanged   chan struct{}
	info           SessionInfo
	lastAccess     time.Time
	device         Device
	process        Process
	output         *outputRing
	done           chan struct{}
	doneOnce       sync.Once
	readerDone     chan struct{}
	readerOnce     sync.Once
	closeRequested bool
	resourceLease  *resourcepool.Lease
}

func NewManager(parent context.Context, options ManagerOptions) (*Manager, error) {
	if parent == nil || options.Launcher == nil || len(options.AllowedRoots) == 0 || options.MaxSessions < 0 {
		return nil, ErrInvalidArgument
	}
	if options.MaxRetainedSessions < 0 || options.MaxRetainedSessions > 256 || options.CompletedRetentionGrace < 0 {
		return nil, ErrInvalidArgument
	}
	if options.MaxRetainedSessions == 0 {
		options.MaxRetainedSessions = 16
	}
	if options.CompletedRetentionGrace == 0 {
		options.CompletedRetentionGrace = 2 * time.Minute
	}
	roots := make([]string, 0, len(options.AllowedRoots))
	for _, root := range options.AllowedRoots {
		canonical, err := canonicalDirectory(root)
		if err != nil {
			return nil, fmt.Errorf("校验终端授权根目录: %w", err)
		}
		roots = append(roots, canonical)
	}
	if options.BufferBytes < 1 {
		options.BufferBytes = 4 << 20
	}
	if options.SnapshotBytes < 1 || options.SnapshotBytes > MaxInputBytes {
		options.SnapshotBytes = MaxInputBytes
	}
	if options.CloseGrace <= 0 {
		options.CloseGrace = 2 * time.Second
	}
	if options.IDGenerator == nil {
		options.IDGenerator = newSessionID
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	ctx, cancel := context.WithCancel(parent)
	return &Manager{
		observer: options.Observer,
		ctx:      ctx, cancel: cancel, launcher: options.Launcher, allowedRoots: roots,
		viewStateFactory: options.ViewStateFactory, maxSessions: options.MaxSessions, bufferBytes: options.BufferBytes, snapshotBytes: options.SnapshotBytes,
		maxRetainedSessions: options.MaxRetainedSessions, completedRetentionGrace: options.CompletedRetentionGrace,
		closeGrace: options.CloseGrace, newID: options.IDGenerator, now: options.Now,
		resourcePool: options.ResourcePool, sessions: make(map[string]*managedSession),
	}, nil
}

func (manager *Manager) Create(ctx context.Context, spec SessionSpec) (created SessionInfo, createErr error) {
	if err := contextError(ctx); err != nil {
		return SessionInfo{}, err
	}
	canonical, err := manager.validateSpec(spec)
	if err != nil {
		return SessionInfo{}, err
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		if manager.observer != nil {
			spec.CWD = canonical
			_ = manager.RecordLaunchFailure(ctx, spec, ErrSessionClosed.Error())
		}
		return SessionInfo{}, ErrSessionClosed
	}
	active := manager.activeCountLocked()
	if manager.maxSessions > 0 && active+manager.pending >= manager.maxSessions {
		manager.mu.Unlock()
		if manager.observer != nil {
			spec.CWD = canonical
			_ = manager.RecordLaunchFailure(ctx, spec, ErrSessionLimit.Error())
		}
		return SessionInfo{}, ErrSessionLimit
	}
	if manager.maxSessions == 0 {
		// 历史缓存只回收已经过保护期且排空输出的终态，不阻止新建会话。
		for retained := len(manager.sessions) - active; retained > manager.maxRetainedSessions; retained-- {
			if !manager.evictCompletedLocked(manager.now()) {
				break
			}
		}
	} else if len(manager.sessions)+manager.pending-manager.maxSessions >= manager.maxRetainedSessions && !manager.evictCompletedLocked(manager.now()) {
		manager.mu.Unlock()
		if manager.observer != nil {
			spec.CWD = canonical
			_ = manager.RecordLaunchFailure(ctx, spec, ErrSessionRetentionBusy.Error())
		}
		return SessionInfo{}, ErrSessionRetentionBusy
	}
	manager.pending++
	manager.mu.Unlock()

	id, err := manager.newID()
	if err != nil {
		manager.finishPending()
		return SessionInfo{}, fmt.Errorf("生成终端会话标识: %w", err)
	}
	tags := cloneObservationTags(spec.Observation)
	intent := SessionInfo{SessionID: id, CWD: canonical, ShellProfile: spec.ShellProfile, Scope: spec.Scope, Title: strings.TrimSpace(spec.Title), State: "starting", CreatedAt: manager.now()}
	manager.observe(ObservationEvent{Kind: "launch_requested", Session: intent, Tags: tags, At: intent.CreatedAt})
	defer func() {
		if createErr != nil {
			manager.observe(ObservationEvent{Kind: "launch_failed", Session: intent, Tags: tags, At: manager.now(), Message: createErr.Error()})
		}
	}()
	lease, err := manager.acquireResource(ctx, id)
	if err != nil {
		manager.finishPending()
		return SessionInfo{}, err
	}
	request := LaunchRequest{CWD: canonical, Cols: spec.Cols, Rows: spec.Rows}
	if spec.Program != nil {
		if spec.Program.Multiplexer != nil {
			value := *spec.Program.Multiplexer
			request.Multiplexer = &value
		}
		request.Executable = spec.Program.Executable
		if spec.Program.Stdin != nil {
			request.Stdin = append([]byte{}, spec.Program.Stdin...)
		}
		request.Arguments = append([]string(nil), spec.Program.Arguments...)
		request.Environment = cloneProgramEnvironment(spec.Program.Environment)
	}
	var state ViewState
	if manager.viewStateFactory != nil {
		state, err = manager.viewStateFactory(spec.Cols, spec.Rows)
		if err == nil && spec.DefaultColors != (DefaultColors{}) {
			setter, ok := state.(interface {
				SetDefaultColors(string, string, string) error
			})
			if !ok {
				err = ErrViewStateUnavailable
			} else {
				err = setter.SetDefaultColors(spec.DefaultColors.Foreground, spec.DefaultColors.Background, spec.DefaultColors.Cursor)
			}
		}
		if err != nil {
			if state != nil {
				state.Close()
			}
			if lease != nil {
				lease.Release()
			}
			manager.finishPending()
			return SessionInfo{}, ErrViewStateUnavailable
		}
	}
	accepted := false
	defer func() {
		if !accepted && state != nil {
			state.Close()
		}
	}()
	device, process, err := manager.launchSession(ctx, request)
	if err != nil {
		if lease != nil {
			lease.Release()
		}
		manager.finishPending()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SessionInfo{}, err
		}
		return SessionInfo{}, fmt.Errorf("启动终端: %w", err)
	}
	now := manager.now()
	session := &managedSession{
		observationTags: tags,
		info: SessionInfo{
			SessionID: id, CWD: canonical, ShellProfile: spec.ShellProfile, Scope: spec.Scope, Title: strings.TrimSpace(spec.Title),
			State: StateRunning, PID: process.PID(), Cols: spec.Cols, Rows: spec.Rows, CreatedAt: now, UpdatedAt: now,
		},
		viewState: state, device: device, process: process, output: newOutputRing(manager.bufferBytes), lastAccess: now,
		ioGate: make(chan struct{}, 1), writeChanged: make(chan struct{}),
		done: make(chan struct{}), readerDone: make(chan struct{}), resourceLease: lease,
	}
	manager.mu.Lock()
	manager.pending--
	if manager.closed {
		manager.mu.Unlock()
		_ = abortTerminalLaunch(process)
		_ = device.Close()
		if lease != nil {
			lease.Release()
		}
		return SessionInfo{}, ErrSessionClosed
	}
	if _, exists := manager.sessions[id]; exists {
		manager.mu.Unlock()
		_ = abortTerminalLaunch(process)
		_ = device.Close()
		if lease != nil {
			lease.Release()
		}
		return SessionInfo{}, errors.New("终端会话标识冲突")
	}
	manager.sessions[id] = session
	if manager.observer != nil {
		manager.observationWG.Add(1)
	}
	manager.mu.Unlock()
	accepted = true
	manager.observe(ObservationEvent{Kind: "started", Session: session.snapshotInfo(), Tags: tags, At: now})
	go manager.readOutput(session)
	go manager.waitProcess(session)
	if manager.observer != nil {
		go manager.observeCompletion(session)
	}
	if lease != nil {
		go manager.watchResourceLease(session)
	}
	return session.snapshotInfo(), nil
}

func (manager *Manager) launchSession(ctx context.Context, request LaunchRequest) (Device, Process, error) {
	type outcome struct {
		device  Device
		process Process
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		device, process, err := manager.launcher.Launch(ctx, request)
		done <- outcome{device: device, process: process, err: err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			result := <-done
			if result.err != nil {
				return
			}
			if result.process != nil {
				_ = abortTerminalLaunch(result.process)
			}
			if result.device != nil {
				_ = result.device.Close()
			}
		}()
		return nil, nil, ctx.Err()
	case result := <-done:
		return result.device, result.process, result.err
	}
}

func (manager *Manager) acquireResource(ctx context.Context, sessionID string) (*resourcepool.Lease, error) {
	if manager.resourcePool == nil {
		return nil, nil
	}
	acquireCtx, cancel := context.WithCancel(manager.ctx)
	stopRequest := context.AfterFunc(ctx, cancel)
	defer func() {
		stopRequest()
		cancel()
	}()
	digest := sha256.Sum256([]byte(sessionID))
	lease, err := manager.resourcePool.Acquire(acquireCtx, resourcepool.LeaseRequest{
		ID:    "terminal-session:" + hex.EncodeToString(digest[:16]),
		Group: resourcepool.GroupTerminal, Priority: resourcepool.PriorityInteractive,
	})
	if err == nil {
		return lease, nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil, err
	case errors.Is(err, resourcepool.ErrQueueFull):
		return nil, ErrSessionLimit
	case errors.Is(err, resourcepool.ErrClosed), errors.Is(err, resourcepool.ErrNotStarted):
		return nil, ErrResourceUnavailable
	default:
		return nil, fmt.Errorf("%w: %v", ErrResourceUnavailable, err)
	}
}

func (manager *Manager) Get(ctx context.Context, sessionID string) (SessionInfo, error) {
	if err := contextError(ctx); err != nil {
		return SessionInfo{}, err
	}
	session, err := manager.readSession(sessionID)
	if err != nil {
		return SessionInfo{}, err
	}
	return session.snapshotInfo(), nil
}

func (manager *Manager) List(ctx context.Context) ([]SessionInfo, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	manager.mu.RLock()
	values := make([]SessionInfo, 0, len(manager.sessions))
	for _, session := range manager.sessions {
		values = append(values, session.snapshotInfo())
	}
	manager.mu.RUnlock()
	sort.Slice(values, func(left, right int) bool {
		if values[left].CreatedAt.Equal(values[right].CreatedAt) {
			return values[left].SessionID < values[right].SessionID
		}
		return values[left].CreatedAt.Before(values[right].CreatedAt)
	})
	return values, nil
}

func (manager *Manager) Write(ctx context.Context, sessionID string, data []byte) error {
	_, err := manager.writeInput(ctx, sessionID, nil, data)
	return err
}

func (manager *Manager) Resize(ctx context.Context, sessionID string, cols, rows int) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !validSize(cols, rows) {
		return ErrInvalidArgument
	}
	session, err := manager.session(sessionID)
	if err != nil {
		return err
	}
	if !session.running() {
		return ErrSessionClosed
	}
	if err := session.acquireIO(ctx, false); err != nil {
		return err
	}
	defer session.releaseIO()
	session.outputMu.Lock()
	defer session.outputMu.Unlock()
	if err := session.device.Resize(cols, rows); err != nil {
		return fmt.Errorf("调整终端尺寸: %w", err)
	}
	if session.viewState != nil && session.viewErr == nil {
		session.viewErr = session.viewState.Resize(cols, rows)
	}
	session.mu.Lock()
	session.info.Cols, session.info.Rows, session.info.UpdatedAt = cols, rows, manager.now()
	session.mu.Unlock()
	return nil
}

func (manager *Manager) Snapshot(ctx context.Context, sessionID string, cursor uint64) (Snapshot, error) {
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	session, err := manager.readSession(sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	output := session.output.snapshot(cursor, manager.snapshotBytes)
	info := session.outputInfo()
	info.Cursor = output.NextCursor
	return Snapshot{
		Session: info, Frames: output.Frames, OldestCursor: output.OldestCursor,
		NextCursor: output.NextCursor, Truncated: output.Truncated, HasMore: output.HasMore,
	}, nil
}

func (manager *Manager) Detach(ctx context.Context, sessionID, reason string) (SessionInfo, error) {
	if err := contextError(ctx); err != nil {
		return SessionInfo{}, err
	}
	if len(reason) > 128 {
		return SessionInfo{}, ErrInvalidArgument
	}
	session, err := manager.session(sessionID)
	if err != nil {
		return SessionInfo{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if terminalState(session.info.State) {
		return SessionInfo{}, ErrSessionClosed
	}
	if session.info.State == StateClosing {
		return SessionInfo{}, ErrSessionClosed
	}
	if session.info.State == StateRunning {
		session.info.State = StateDetached
		session.info.UpdatedAt = manager.now()
	}
	return cloneSessionInfo(session.info), nil
}

func (manager *Manager) Attach(ctx context.Context, sessionID string, cursor uint64) (Snapshot, error) {
	if err := contextError(ctx); err != nil {
		return Snapshot{}, err
	}
	session, err := manager.session(sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	session.mu.Lock()
	if terminalState(session.info.State) || session.info.State == StateClosing {
		session.mu.Unlock()
		return Snapshot{}, ErrSessionClosed
	}
	if session.info.State == StateDetached {
		session.info.State = StateRunning
		session.info.UpdatedAt = manager.now()
	}
	session.mu.Unlock()
	return manager.Snapshot(ctx, sessionID, cursor)
}

func (manager *Manager) CloseSession(ctx context.Context, sessionID, reason string) error {
	return manager.closeSession(ctx, sessionID, reason, false)
}

func (manager *Manager) closeSession(ctx context.Context, sessionID, reason string, detach bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if len(reason) > 128 {
		return ErrInvalidArgument
	}
	session, err := manager.session(sessionID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	if session.info.State == StateClosed {
		session.mu.Unlock()
		return nil
	}
	if terminalState(session.info.State) {
		// 进程已退出或中断时，用户关闭仍要记成 closed，否则桌面刷新会把 exited Tab 再列出来。
		session.info.State = StateClosed
		session.info.UpdatedAt = manager.now()
		session.mu.Unlock()
		return nil
	}
	if session.info.State != StateClosing {
		session.info.State = StateClosing
		session.info.UpdatedAt = manager.now()
		session.closeRequested = true
		session.notifyInputChangedLocked()
	}
	session.mu.Unlock()
	interrupt, kill := session.process.Interrupt, session.process.KillTree
	if owner, ok := session.process.(interface{ Detach() error }); detach && ok {
		interrupt, kill = owner.Detach, owner.Detach
	}
	_ = interrupt()

	timer := time.NewTimer(manager.closeGrace)
	defer timer.Stop()
	select {
	case <-session.done:
	case <-ctx.Done():
		_ = kill()
		return ctx.Err()
	case <-timer.C:
		if err := kill(); err != nil {
			return fmt.Errorf("强制关闭终端进程树: %w", err)
		}
		select {
		case <-session.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return waitReaderDone(ctx, session)
}

func (manager *Manager) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	manager.cancel()
	ids := make([]string, 0, len(manager.sessions))
	for id, session := range manager.sessions {
		if !terminalState(session.snapshotInfo().State) {
			ids = append(ids, id)
		}
	}
	manager.mu.Unlock()
	var result error
	for _, id := range ids {
		result = errors.Join(result, manager.closeSession(ctx, id, "manager-close", true))
	}
	manager.mu.RLock()
	for _, session := range manager.sessions {
		session.closeView()
	}
	manager.mu.RUnlock()
	if manager.observer != nil {
		done := make(chan struct{})
		go func() { manager.observationWG.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			result = errors.Join(result, ctx.Err())
		}
	}
	return result
}

func (manager *Manager) validateSpec(spec SessionSpec) (string, error) {
	if !spec.DefaultColors.valid() {
		return "", ErrInvalidArgument
	}
	if spec.ShellProfile != "system" || !ValidScope(spec.Scope) || len(strings.TrimSpace(spec.Title)) > 128 || !validSize(spec.Cols, spec.Rows) {
		return "", ErrInvalidArgument
	}
	if spec.Program != nil {
		if err := validateProgram(*spec.Program); err != nil {
			return "", err
		}
	}
	workingDirectory := spec.CWD
	if workingDirectory == "" {
		workingDirectory = manager.allowedRoots[0]
	}
	canonical, err := canonicalDirectory(workingDirectory)
	if err != nil {
		return "", ErrPathNotGranted
	}
	for _, root := range manager.allowedRoots {
		if pathWithin(root, canonical) {
			return canonical, nil
		}
	}
	if spec.PrevalidatedCWD {
		return canonical, nil
	}
	return "", ErrPathNotGranted
}

func validProgram(program ProgramSpec) bool {
	return validateProgram(program) == nil
}

func validateProgram(program ProgramSpec) error {
	if program.Multiplexer != nil && (program.Stdin != nil || !validMultiplexer(*program.Multiplexer)) {
		return ErrInvalidArgument
	}
	if program.Stdin != nil && (!SupportsProgramStdin() || len(program.Stdin) > MaxInputBytes) {
		return ErrInvalidArgument
	}
	if !filepath.IsAbs(program.Executable) || len(program.Executable) > 4096 || containsProgramControl(program.Executable) || len(program.Arguments) > 64 {
		return ErrInvalidArgument
	}
	for index, argument := range program.Arguments {
		check := argument
		if program.LiteralLastArgument && index == len(program.Arguments)-1 {
			check = strings.NewReplacer("\n", "", "\t", "").Replace(check)
		}
		if len(argument) > MaxInputBytes || containsProgramControl(check) {
			return ErrInvalidArgument
		}
	}
	return validateProgramEnvironment(program.Environment)
}

func containsProgramControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func (manager *Manager) session(sessionID string) (*managedSession, error) {
	if sessionID == "" {
		return nil, ErrInvalidArgument
	}
	manager.mu.RLock()
	session := manager.sessions[sessionID]
	manager.mu.RUnlock()
	if session == nil {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

func (manager *Manager) finishPending() {
	manager.mu.Lock()
	manager.pending--
	manager.mu.Unlock()
}

func (manager *Manager) activeCountLocked() int {
	count := 0
	for _, session := range manager.sessions {
		if !terminalState(session.snapshotInfo().State) {
			count++
		}
	}
	return count
}

func (session *managedSession) markReaderDone() {
	session.readerOnce.Do(func() { close(session.readerDone) })
}

func waitReaderDone(ctx context.Context, session *managedSession) error {
	if session.readerDone == nil {
		return nil
	}
	select {
	case <-session.readerDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (manager *Manager) readOutput(session *managedSession) {
	defer session.markReaderDone()
	defer session.finishView()
	buffer := make([]byte, 32<<10)
	pending := make([]byte, 0, utf8.UTFMax)
	for {
		count, err := session.device.Read(buffer)
		if count > 0 {
			pending = append(pending, buffer[:count]...)
			complete := completeUTF8Prefix(pending)
			if complete > 0 {
				output := bytes.ToValidUTF8(pending[:complete], []byte(string(utf8.RuneError)))
				manager.appendOutput(session, output)
				session.touch(manager.now())
				pending = append(pending[:0], pending[complete:]...)
			}
		}
		if err != nil {
			if len(pending) > 0 {
				manager.appendOutput(session, bytes.ToValidUTF8(pending, []byte(string(utf8.RuneError))))
				session.touch(manager.now())
			}
			return
		}
	}
}

func completeUTF8Prefix(data []byte) int {
	index := 0
	for index < len(data) {
		if !utf8.FullRune(data[index:]) {
			break
		}
		_, size := utf8.DecodeRune(data[index:])
		index += size
	}
	return index
}

func (manager *Manager) waitProcess(session *managedSession) {
	code, err := session.process.Wait()
	_ = session.device.Close()
	now := manager.now()
	session.mu.Lock()
	session.info.ExitCode = intPointer(code)
	session.info.ExitedAt = timePointer(now)
	session.info.UpdatedAt = now
	switch {
	case session.closeRequested:
		session.info.State = StateClosed
	case err != nil && code < 0:
		session.info.State = StateInterrupted
	default:
		session.info.State = StateExited
	}
	session.lastAccess = now
	session.notifyInputChangedLocked()
	session.mu.Unlock()
	session.doneOnce.Do(func() { close(session.done) })
	if session.resourceLease != nil {
		session.resourceLease.Release()
	}
}

func (manager *Manager) watchResourceLease(session *managedSession) {
	select {
	case <-session.done:
		return
	case <-session.resourceLease.Done():
		if !terminalState(session.snapshotInfo().State) {
			// 整个资源池退出只撤销连接额度；显式取消单个 lease 仍终止程序。
			if owner, ok := session.process.(interface{ Detach() error }); ok && errors.Is(manager.resourcePool.Ready(context.Background()), resourcepool.ErrClosed) {
				_ = owner.Detach()
				return
			}
			_ = session.process.KillTree()
		}
	}
}

func (session *managedSession) snapshotInfo() SessionInfo {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return cloneSessionInfo(session.info)
}

func (session *managedSession) running() bool {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.info.State == StateRunning
}

func cloneSessionInfo(info SessionInfo) SessionInfo {
	result := info
	if info.ExitCode != nil {
		result.ExitCode = intPointer(*info.ExitCode)
	}
	if info.ExitedAt != nil {
		result.ExitedAt = timePointer(*info.ExitedAt)
	}
	return result
}

func (session *managedSession) touch(now time.Time) {
	session.mu.Lock()
	session.info.UpdatedAt = now
	session.mu.Unlock()
}

func canonicalDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", ErrInvalidArgument
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	fileInfo, err := os.Stat(resolved)
	if err != nil || !fileInfo.IsDir() {
		return "", ErrInvalidArgument
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validSize(cols, rows int) bool {
	return cols >= minCols && cols <= maxCols && rows >= minRows && rows <= maxRows
}

func terminalState(state string) bool {
	return state == StateClosed || state == StateExited || state == StateInterrupted
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	return ctx.Err()
}

func newSessionID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "term-" + hex.EncodeToString(buffer), nil
}

func intPointer(value int) *int              { return &value }
func timePointer(value time.Time) *time.Time { return &value }
