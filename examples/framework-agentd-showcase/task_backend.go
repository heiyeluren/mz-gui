package main

import "context"

type TaskSnapshot struct {
	ID                string `json:"id"`
	State             string `json:"state"`
	ResultSummaryJSON string `json:"result_summary_json"`
}

type TaskBackend interface {
	StartTask(context.Context, string, string) (TaskSnapshot, error)
	GetTask(context.Context, string) (TaskSnapshot, error)
	CancelTask(context.Context, string, string, string) (TaskSnapshot, error)
}
