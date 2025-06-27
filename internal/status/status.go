package status

type Status = int32

const (
	Pending Status = iota
	Active
	Paused
	Completed
	Failed
	Queued
	Cancelled
)
