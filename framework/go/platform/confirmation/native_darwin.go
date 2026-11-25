//go:build darwin

package confirmation

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

const nativeConfirmationAppleScript = `on run argv
  set dialogTitle to item 1 of argv
  set dialogMessage to item 2 of argv
  set confirmLabel to item 3 of argv
  set cancelLabel to item 4 of argv
  try
    set answer to display dialog dialogMessage with title dialogTitle buttons {cancelLabel, confirmLabel} default button confirmLabel cancel button cancelLabel with icon caution
    return button returned of answer
  on error number -128
    return "__MZ_CANCEL__"
  end try
end run`

func nativeConfirm(ctx context.Context, request Request) (bool, error) {
	command := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", nativeConfirmationAppleScript, request.Title, request.Message, request.ConfirmLabel, request.CancelLabel)
	output, err := command.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, ctx.Err()
		}
		return false, ErrUnavailable
	}
	result := strings.TrimSpace(string(output))
	if result == "__MZ_CANCEL__" || result == request.CancelLabel {
		return false, nil
	}
	if result != request.ConfirmLabel {
		return false, ErrUnavailable
	}
	return true, nil
}
