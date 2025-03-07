package downloader

import (
	"context"
	"sync"
	"time"
)

// Downloader defines the interface for download operations
type Downloader interface {
	// Start initiates the download with the given context
	Start(ctx context.Context) error
	// Pause temporarily halts the download while maintaining progress
	Pause() error
	// Resume continues a paused download
	Resume(ctx context.Context) error
	// Cancel aborts the download and optionally removes partial files
	Cancel(removePartialFiles bool) error
	// GetStats returns current statistics about the download
	GetStats() DownloadStats
	// GetProgressChan returns a channel for real-time progress updates
	GetProgressChan() <-chan Progress
}

// Download represents a file download job
type Download struct {
	ID             string
	URL            string
	Filename       string
	Options        DownloadOptions
	Status         DownloadStatus
	Error          error
	TotalSize      int64
	Downloaded     int64 // Atomic
	StartTime      time.Time
	EndTime        time.Time
	SpeedSamples   []int64
	LastSpeedCheck time.Time
	BytesSinceLast int64 // Atomic

	// Concurrency control
	mu         sync.RWMutex
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Channel for real-time progress updates
	progressCh chan Progress
}
