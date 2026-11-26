package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type DesktopService struct {
	backend TaskBackend
}

func (service *DesktopService) StartTask(payload string) (TaskSnapshot, error) {
	return service.backend.StartTask(context.Background(), payload, newOperationID("start"))
}

func (service *DesktopService) GetTask(taskID string) (TaskSnapshot, error) {
	return service.backend.GetTask(context.Background(), taskID)
}

func (service *DesktopService) CancelTask(taskID string) (TaskSnapshot, error) {
	return service.backend.CancelTask(context.Background(), taskID, "用户从桌面取消", newOperationID("cancel"))
}

func newOperationID(prefix string) string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "-" + hex.EncodeToString(value)
}
