package task

import (
	"errors"

	"sis/analyzer"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

var (
	ErrNotFound          = errors.New("task not found")
	ErrInvalidTransition = errors.New("invalid status transition")
)

type Task struct {
	ID       int
	Text     string
	Status   Status
	Result   analyzer.Result
	Error    string
	WorkerID int
}
