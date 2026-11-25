package confirmation

import "context"

// NativeConfirmer 可由无窗口后台进程调用，确认结果直接返回可信服务进程。
type NativeConfirmer struct{}

func (NativeConfirmer) Confirm(ctx context.Context, request Request) (bool, error) {
	if err := request.Validate(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return nativeConfirm(ctx, request)
}
