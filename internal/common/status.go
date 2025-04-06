package common

type Status = int32

const (
	StatusPending Status = iota
	StatusActive
	StatusPaused
	StatusCompleted
	StatusFailed
	StatusQueued
	StatusMerging
	StatusCancelled
)
