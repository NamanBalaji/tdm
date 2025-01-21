package event

import "time"

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

	ChunkStarted
	ChunkCompleted
	ChunkFailed

	StorageError
	NetworkError
)

type Event struct {
	Type      EventType
	Payload   any
	Timestamp time.Time
}

type Subscription struct {
	Types []EventType
	Ch    chan Event
	Done  chan struct{}
}
