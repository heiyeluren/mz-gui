package undoredo

import (
	"context"
	"errors"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
)

const defaultMaxDepth = 100

var (
	ErrInvalidAction = errors.New("可逆动作无效")
	ErrEmptyStack    = errors.New("可逆动作栈为空")
	ErrNotReversible = errors.New("动作不可逆")
)

type Options struct {
	MaxDepth int
}

type Action struct {
	ID      string
	Label   string
	Meta    inputmodel.RequestMeta
	Do      func(context.Context) error
	Undo    func(context.Context) error
	Redo    func(context.Context) error
	CanUndo bool
}

type Snapshot struct {
	CanUndo       bool
	CanRedo       bool
	UndoDepth     int
	RedoDepth     int
	NextUndoID    string
	NextRedoID    string
	NextUndoLabel string
	NextRedoLabel string
}

type Stack struct {
	mu       sync.Mutex
	maxDepth int
	undo     []Action
	redo     []Action
}

func New(options Options) *Stack {
	maxDepth := options.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	return &Stack{maxDepth: maxDepth}
}

func (stack *Stack) Execute(ctx context.Context, action Action) error {
	if ctx == nil {
		return ErrInvalidAction
	}
	if err := validateAction(action); err != nil {
		return err
	}
	if err := action.Do(ctx); err != nil {
		return err
	}
	stack.mu.Lock()
	defer stack.mu.Unlock()
	stack.undo = append(stack.undo, action)
	if len(stack.undo) > stack.maxDepth {
		stack.undo = append([]Action(nil), stack.undo[len(stack.undo)-stack.maxDepth:]...)
	}
	stack.redo = nil
	return nil
}

func (stack *Stack) Record(action Action) error {
	if err := validateAction(action); err != nil {
		return err
	}
	stack.mu.Lock()
	defer stack.mu.Unlock()
	stack.undo = append(stack.undo, action)
	if len(stack.undo) > stack.maxDepth {
		stack.undo = append([]Action(nil), stack.undo[len(stack.undo)-stack.maxDepth:]...)
	}
	stack.redo = nil
	return nil
}

func (stack *Stack) Undo(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidAction
	}
	stack.mu.Lock()
	if len(stack.undo) == 0 {
		stack.mu.Unlock()
		return ErrEmptyStack
	}
	action := stack.undo[len(stack.undo)-1]
	stack.undo = stack.undo[:len(stack.undo)-1]
	stack.mu.Unlock()
	if err := action.Undo(ctx); err != nil {
		stack.mu.Lock()
		stack.undo = append(stack.undo, action)
		stack.mu.Unlock()
		return err
	}
	stack.mu.Lock()
	stack.redo = append(stack.redo, action)
	stack.mu.Unlock()
	return nil
}

func (stack *Stack) Redo(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidAction
	}
	stack.mu.Lock()
	if len(stack.redo) == 0 {
		stack.mu.Unlock()
		return ErrEmptyStack
	}
	action := stack.redo[len(stack.redo)-1]
	stack.redo = stack.redo[:len(stack.redo)-1]
	stack.mu.Unlock()
	redo := action.Do
	if action.Redo != nil {
		redo = action.Redo
	}
	if err := redo(ctx); err != nil {
		stack.mu.Lock()
		stack.redo = append(stack.redo, action)
		stack.mu.Unlock()
		return err
	}
	stack.mu.Lock()
	stack.undo = append(stack.undo, action)
	if len(stack.undo) > stack.maxDepth {
		stack.undo = append([]Action(nil), stack.undo[len(stack.undo)-stack.maxDepth:]...)
	}
	stack.mu.Unlock()
	return nil
}

func (stack *Stack) Snapshot() Snapshot {
	stack.mu.Lock()
	defer stack.mu.Unlock()
	snapshot := Snapshot{
		CanUndo:   len(stack.undo) > 0,
		CanRedo:   len(stack.redo) > 0,
		UndoDepth: len(stack.undo),
		RedoDepth: len(stack.redo),
	}
	if snapshot.CanUndo {
		action := stack.undo[len(stack.undo)-1]
		snapshot.NextUndoID = action.ID
		snapshot.NextUndoLabel = action.Label
	}
	if snapshot.CanRedo {
		action := stack.redo[len(stack.redo)-1]
		snapshot.NextRedoID = action.ID
		snapshot.NextRedoLabel = action.Label
	}
	return snapshot
}

func (snapshot Snapshot) Speak() string {
	if snapshot.CanUndo && snapshot.NextUndoLabel != "" {
		return "撤销，" + snapshot.NextUndoLabel
	}
	return "无法撤销"
}

func (snapshot Snapshot) SpeakRedo() string {
	if snapshot.CanRedo && snapshot.NextRedoLabel != "" {
		return "重做，" + snapshot.NextRedoLabel
	}
	return "无法重做"
}

func validateAction(action Action) error {
	if action.ID == "" || action.Do == nil {
		return ErrInvalidAction
	}
	if err := action.Meta.Validate(); err != nil {
		return err
	}
	if !action.CanUndo || action.Undo == nil {
		return ErrNotReversible
	}
	return nil
}
