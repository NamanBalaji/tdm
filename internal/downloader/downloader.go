package downloader

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/google/uuid"
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
	ID             uuid.UUID       `json:"id"`
	URL            string          `json:"url"`
	Filename       string          `json:"filename"`
	Options        DownloadOptions `json:"options"`
	Status         DownloadStatus  `json:"status"`
	Error          error           `json:"-"`
	TotalSize      int64           `json:"total_size"`
	Downloaded     int64           `json:"downloaded"`
	Chunks         []*chunk.Chunk  `json:"chunks"`
	StartTime      time.Time       `json:"start_time,omitempty"`
	EndTime        time.Time       `json:"end_time,omitempty"`
	SpeedSamples   []int64         `json:"_"`
	LastSpeedCheck time.Time       `json:"-"`
	BytesSinceLast int64           `json:"-"`

	// Concurrency control
	mu         sync.RWMutex       `json:"-"`
	ctx        context.Context    `json:"-"`
	cancelFunc context.CancelFunc `json:"-"`

	// Channel for real-time progress updates
	progressCh chan Progress `json:"-"`
}

// NewDownload creates a new Download instance
func NewDownload(url, filename string, options *DownloadOptions) *Download {
	return &Download{
		ID:             uuid.New(),
		URL:            url,
		Filename:       filename,
		Options:        *options,
		Status:         StatusPending,
		progressCh:     make(chan Progress),
		StartTime:      time.Now(),
		LastSpeedCheck: time.Now(),
	}
}

// Start initiates the download
func (d *Download) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Create a cancellable context
	d.ctx, d.cancelFunc = context.WithCancel(ctx)

	// Update status
	d.Status = StatusActive
	d.StartTime = time.Now()

	// This is just a stub - actual implementation will be in the download manager
	return nil
}

// Pause temporarily stops the download but keeps progress
func (d *Download) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Status != StatusActive {
		return nil // No-op if not active
	}

	d.Status = StatusPaused

	// Cancel ongoing operations (will be implemented later)
	if d.cancelFunc != nil {
		d.cancelFunc()
	}

	return nil
}

// Resume continues a paused download
func (d *Download) Resume(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.Status != StatusPaused {
		return nil // No-op if not paused
	}

	// Create a new cancellable context
	d.ctx, d.cancelFunc = context.WithCancel(ctx)
	d.Status = StatusActive

	// Actual resume logic will be implemented later
	return nil
}

// Cancel stops the download and optionally removes partial files
func (d *Download) Cancel(removePartialFiles bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cannot cancel completed downloads
	if d.Status == StatusCompleted {
		return nil
	}

	d.Status = StatusFailed

	// Cancel ongoing operations
	if d.cancelFunc != nil {
		d.cancelFunc()
	}

	// Actual cleanup logic will be implemented later
	return nil
}

// GetStats returns the current download statistics
func (d *Download) GetStats() DownloadStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Calculate progress percentage
	var progress float64 = 0
	if d.TotalSize > 0 {
		progress = float64(atomic.LoadInt64(&d.Downloaded)) / float64(d.TotalSize) * 100
	}

	// Calculate current speed
	currentSpeed := d.calculateSpeed()

	// Count chunks by status
	activeChunks := 0
	completedChunks := 0
	totalChunks := len(d.Chunks)

	for _, c := range d.Chunks {
		if c.Status == chunk.Active {
			activeChunks++
		} else if c.Status == chunk.Completed {
			completedChunks++
		}
	}

	// Calculate time remaining
	var timeRemaining time.Duration
	if currentSpeed > 0 {
		bytesRemaining := d.TotalSize - atomic.LoadInt64(&d.Downloaded)
		if bytesRemaining > 0 {
			timeRemaining = time.Duration(bytesRemaining/currentSpeed) * time.Second
		}
	}

	// Create and return stats
	errorStr := ""
	if d.Error != nil {
		errorStr = d.Error.Error()
	}

	return DownloadStats{
		Status:          d.Status,
		TotalSize:       d.TotalSize,
		Downloaded:      atomic.LoadInt64(&d.Downloaded),
		Speed:           currentSpeed,
		Progress:        progress,
		ActiveChunks:    activeChunks,
		CompletedChunks: completedChunks,
		TotalChunks:     totalChunks,
		TimeElapsed:     time.Since(d.StartTime),
		TimeRemaining:   timeRemaining,
		Error:           errorStr,
		StartTime:       d.StartTime,
		LastUpdated:     time.Now(),
		Connections:     d.countActiveConnections(),
	}
}

// GetProgressChannel returns the channel for progress updates
func (d *Download) GetProgressChannel() <-chan Progress {
	return d.progressCh
}

// AddProgress adds the specified number of bytes to the download progress
func (d *Download) AddProgress(bytes int64) {
	atomic.AddInt64(&d.Downloaded, bytes)
	atomic.AddInt64(&d.BytesSinceLast, bytes)

	// Send progress update
	select {
	case d.progressCh <- Progress{
		DownloadID:     d.ID,
		BytesCompleted: atomic.LoadInt64(&d.Downloaded),
		TotalBytes:     d.TotalSize,
		Speed:          d.calculateSpeed(),
		Status:         d.Status,
		Timestamp:      time.Now(),
	}:
		// Update sent successfully
	default:
		// Channel buffer is full, skip this update
	}
}

// calculateSpeed calculates the current download speed in bytes/sec
func (d *Download) calculateSpeed() int64 {
	now := time.Now()
	elapsed := now.Sub(d.LastSpeedCheck)

	// Only recalculate speed after a reasonable interval (e.g., 1 second)
	if elapsed < time.Second {
		// Return the last calculated speed if available
		if len(d.SpeedSamples) > 0 {
			return d.SpeedSamples[len(d.SpeedSamples)-1]
		}
		return 0
	}

	// Calculate bytes per second
	bytesSinceLast := atomic.SwapInt64(&d.BytesSinceLast, 0)
	speed := int64(float64(bytesSinceLast) / elapsed.Seconds())

	// Update speed samples (keep last 5 samples for smoothing)
	d.mu.Lock()
	d.SpeedSamples = append(d.SpeedSamples, speed)
	if len(d.SpeedSamples) > 5 {
		d.SpeedSamples = d.SpeedSamples[1:]
	}
	d.LastSpeedCheck = now
	d.mu.Unlock()

	// Return the average of recent samples for smoother readings
	return d.getAverageSpeed()
}

// getAverageSpeed calculates the average of recent speed samples
func (d *Download) getAverageSpeed() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.SpeedSamples) == 0 {
		return 0
	}

	var sum int64
	for _, speed := range d.SpeedSamples {
		sum += speed
	}

	return sum / int64(len(d.SpeedSamples))
}

// countActiveConnections counts the number of active connections across all chunks
func (d *Download) countActiveConnections() int {
	count := 0
	for _, c := range d.Chunks {
		// @TODO: check if chunk conn != nil
		if c.Status == chunk.Active {
			count++
		}
	}
	return count
}
