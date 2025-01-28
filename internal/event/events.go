package event

import (
	"sync/atomic"
	"time"
)

type EventType uint8

// Define all event types.
const (
	TaskCreated EventType = iota
	TaskStarted
	TaskPaused
	TaskResumed
	TaskCompleted
	TaskFailed
	TaskDeleted
	TaskPriorityChanged
	ChunkCreated
	ChunkStarted
	ChunkProgress
	ChunkCompleted
	ChunkFailed
	ChunkRetry
	FileMergingStarted
	FileMergingCompleted
	FileMergingFailed
	BandwidthThrottled
	BandwidthUnthrottled
	SystemError
	SystemWarning
	DownloadQueueUpdated
	SessionSaved
	SessionRestored
)

// Event represents a single event with a type, data, and timestamp.
type Event struct {
	Type      EventType
	Data      any
	Timestamp time.Time
}

// subscriber represents a single subscriber with a channel and a closed flag.
type subscriber struct {
	ch     chan Event
	closed atomic.Bool
}
