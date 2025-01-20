// internal/engine/events/types.go
package events

import (
	"time"

	"github.com/google/uuid"
)

type EventType uint8

const (
	DownloadCreated EventType = iota
	DownloadStarted
	DownloadPaused
	DownloadResumed
	DownloadCompleted
	DownloadFailed
	DownloadCancelled

	DownloadProgress
	PieceStarted
	PieceCompleted
	PieceFailed

	StorageError
	NetworkError
)

type Event struct {
	Type      EventType
	ID        uuid.UUID
	Timestamp time.Time
	Data      any
	Error     error
}

type Subscription struct {
	Types  []EventType
	Ch     chan Event
	closed bool
}

type DownloadProgressData struct {
	BytesComplete   int64
	BytesTotal      int64
	CurrentSpeed    float64
	AverageSpeed    float64
	TimeRemaining   time.Duration
	PercentComplete float64
}

type PieceProgressData struct {
	PieceIndex    int
	BytesComplete int64
	BytesTotal    int64
	Speed         float64
	TimeElapsed   time.Duration
}

type ErrorData struct {
	Operation  string
	Message    string
	Retryable  bool
	RetryCount int
}
