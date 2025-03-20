package downloader

import (
	"context"
	"github.com/NamanBalaji/tdm/internal/common"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/google/uuid"
)

// Download represents a file download job
type Download struct {
	ID             uuid.UUID       `json:"id"`
	URL            string          `json:"url"`
	Filename       string          `json:"filename"`
	Options        DownloadOptions `json:"options"`
	Status          common.Status  `json:"status"`
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
	progressCh chan common.Progress `json:"-"`
}

// NewDownload creates a new Download instance
func NewDownload(url, filename string, options *DownloadOptions) *Download {
	return &Download{
		ID:             uuid.New(),
		URL:            url,
		Filename:       filename,
		Options:        *options,
		Status:         common.StatusPending,
		progressCh:     make(chan common.Progress),
		StartTime:      time.Now(),
		LastSpeedCheck: time.Now(),
	}
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
		if c.Status == common.StatusActive {
			activeChunks++
		} else if c.Status == common.StatusCompleted {
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

// SetContext sets the download context and cancel function
func (d *Download) SetContext(ctx context.Context, cancelFunc context.CancelFunc) {
	d.ctx = ctx
	d.cancelFunc = cancelFunc
}

// Context returns the download context
func (d *Download) Context() context.Context {
	return d.ctx
}

// CancelFunc returns the cancel function
func (d *Download) CancelFunc() context.CancelFunc {
	return d.cancelFunc
}

// GetProgressChannel returns the channel for progress updates
func (d *Download) GetProgressChannel() chan common.Progress {
	return d.progressCh
}

// AddProgress adds the specified number of bytes to the download progress
func (d *Download) AddProgress(bytes int64) {
	atomic.AddInt64(&d.Downloaded, bytes)
	atomic.AddInt64(&d.BytesSinceLast, bytes)

	// Send progress update
	select {
	case d.progressCh <- common.Progress{
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
		if c.Status == common.StatusActive {
			count++
		}
	}
	return count
}
