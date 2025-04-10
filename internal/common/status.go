package common

import "fmt"

type Status int32

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

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "Pending"
	case StatusActive:
		return "Active"
	case StatusPaused:
		return "Paused"
	case StatusCompleted:
		return "Completed"
	case StatusFailed:
		return "Failed"
	case StatusQueued:
		return "Queued"
	case StatusMerging:
		return "Merging"
	case StatusCancelled:
		return "Cancelled"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}
