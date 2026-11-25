package commandundo

import (
	"context"
	"errors"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/commandbus"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/undoredo"
)

const (
	CommandUndo = "interaction.undo"
	CommandRedo = "interaction.redo"
)

var ErrInvalidAdapter = errors.New("可逆命令 adapter 配置无效")

type AdapterOptions struct {
	Bus   *commandbus.Bus
	Stack *undoredo.Stack
}

type Handler func(context.Context, commandbus.Command) (commandbus.Result, undoredo.Action, error)

type Adapter struct {
	bus   *commandbus.Bus
	stack *undoredo.Stack
}

func New(options AdapterOptions) (*Adapter, error) {
	if options.Bus == nil || options.Stack == nil {
		return nil, ErrInvalidAdapter
	}
	return &Adapter{bus: options.Bus, stack: options.Stack}, nil
}

func (adapter *Adapter) Register(commandType string, handler Handler) error {
	if adapter == nil || adapter.bus == nil || commandType == "" || handler == nil {
		return ErrInvalidAdapter
	}
	return adapter.bus.Register(commandType, func(ctx context.Context, command commandbus.Command) (commandbus.Result, error) {
		result, action, err := handler(ctx, command)
		if err != nil {
			return commandbus.Result{}, err
		}
		if action.Undo == nil {
			return result, nil
		}
		if action.ID == "" {
			action.ID = command.Meta.RequestID
		}
		if action.Meta.RequestID == "" {
			action.Meta = command.Meta
		}
		action.CanUndo = true
		if action.Redo == nil && action.Do != nil {
			action.Redo = action.Do
		}
		if action.Do == nil {
			action.Do = func(context.Context) error { return nil }
		}
		if err := adapter.stack.Record(action); err != nil {
			return commandbus.Result{}, err
		}
		return result, nil
	})
}

func (adapter *Adapter) RegisterStackCommands() error {
	if adapter == nil || adapter.bus == nil || adapter.stack == nil {
		return ErrInvalidAdapter
	}
	if err := adapter.bus.Register(CommandUndo, func(ctx context.Context, command commandbus.Command) (commandbus.Result, error) {
		if err := adapter.stack.Undo(ctx); err != nil {
			return commandbus.Result{}, err
		}
		return commandbus.Result{CommandType: CommandUndo, RequestID: command.Meta.RequestID}, nil
	}); err != nil {
		return err
	}
	return adapter.bus.Register(CommandRedo, func(ctx context.Context, command commandbus.Command) (commandbus.Result, error) {
		if err := adapter.stack.Redo(ctx); err != nil {
			return commandbus.Result{}, err
		}
		return commandbus.Result{CommandType: CommandRedo, RequestID: command.Meta.RequestID}, nil
	})
}

func (adapter *Adapter) Dispatch(ctx context.Context, command commandbus.Command) (commandbus.Result, error) {
	if adapter == nil || adapter.bus == nil {
		return commandbus.Result{}, ErrInvalidAdapter
	}
	return adapter.bus.Dispatch(ctx, command)
}

func (adapter *Adapter) Undo(ctx context.Context) error {
	if adapter == nil || adapter.stack == nil {
		return ErrInvalidAdapter
	}
	return adapter.stack.Undo(ctx)
}

func (adapter *Adapter) Redo(ctx context.Context) error {
	if adapter == nil || adapter.stack == nil {
		return ErrInvalidAdapter
	}
	return adapter.stack.Redo(ctx)
}

func (adapter *Adapter) Snapshot() undoredo.Snapshot {
	if adapter == nil || adapter.stack == nil {
		return undoredo.Snapshot{}
	}
	return adapter.stack.Snapshot()
}
