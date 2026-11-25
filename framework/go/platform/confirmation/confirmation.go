package confirmation

import (
	"context"
	"errors"
	"strings"
)

var ErrUnavailable = errors.New("系统确认对话框不可用")

type Request struct {
	Title        string
	Message      string
	ConfirmLabel string
	CancelLabel  string
}

type Confirmer interface {
	Confirm(context.Context, Request) (bool, error)
}

func (request Request) Validate() error {
	values := []string{request.Title, request.Message, request.ConfirmLabel, request.CancelLabel}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 500 {
			return errors.New("系统确认内容无效")
		}
	}
	return nil
}
