package common

type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusQueued    Status = "queued"
	StatusMerging   Status = "merging"
	StatusCancelled Status = "cancelled"
)
