package downloader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/google/uuid"
)

// Download represents a file download task
type Download struct {
	// Core identifying information
	ID       uuid.UUID `json:"id"`
	URL      string    `json:"url"`      // Source URL
	Filename string    `json:"filename"` // Target filename

	Config    *Config       `json:"config"`     // Download configuration
	Status    common.Status `json:"status"`     // Current status
	TotalSize int64         `json:"total_size"` // Total file size in bytes

	Downloaded int64     `json:"downloaded"` // Downloaded bytes so far
	StartTime  time.Time `json:"start_time,omitempty"`
	EndTime    time.Time `json:"end_time,omitempty"`

	ChunkInfos []common.ChunkInfo `json:"chunk_infos"` // Chunk data for serialization
	Chunks     []*chunk.Chunk     `json:"-"`           // Actual chunks (runtime only)

	ErrorMessage string `json:"error_message,omitempty"` // For persistent storage
	Error        error  `json:"-"`                       // Runtime only

	mu              sync.RWMutex         `json:"-"`
	ctx             context.Context      `json:"-"`
	cancelFunc      context.CancelFunc   `json:"-"`
	progressCh      chan common.Progress `json:"-"`
	speedCalculator *SpeedCalculator     `json:"-"`
}

// NewDownload creates a new Download instance
func NewDownload(url, filename string, config *Config) *Download {
	return &Download{
		ID:              uuid.New(),
		URL:             url,
		Filename:        filename,
		Config:          config,
		Status:          common.StatusPending,
		ChunkInfos:      make([]common.ChunkInfo, 0),
		Chunks:          make([]*chunk.Chunk, 0),
		progressCh:      make(chan common.Progress, 10),
		speedCalculator: NewSpeedCalculator(5),
		StartTime:       time.Now(),
	}
}

// GetStats returns current download statistics
func (d *Download) GetStats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var progress float64
	if d.TotalSize > 0 {
		progress = float64(atomic.LoadInt64(&d.Downloaded)) / float64(d.TotalSize) * 100
	}

	var speed int64
	if d.speedCalculator != nil {
		speed = d.speedCalculator.GetSpeed()
	}

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

	timeElapsed := time.Since(d.StartTime)
	var timeRemaining time.Duration
	if speed > 0 {
		bytesRemaining := d.TotalSize - atomic.LoadInt64(&d.Downloaded)
		if bytesRemaining > 0 {
			timeRemaining = time.Duration(bytesRemaining/speed) * time.Second
		}
	}

	errorMsg := ""
	if d.Error != nil {
		errorMsg = d.Error.Error()
	} else if d.ErrorMessage != "" {
		errorMsg = d.ErrorMessage
	}

	return Stats{
		ID:              d.ID,
		Status:          d.Status,
		TotalSize:       d.TotalSize,
		Downloaded:      atomic.LoadInt64(&d.Downloaded),
		Progress:        progress,
		Speed:           speed,
		TimeElapsed:     timeElapsed,
		TimeRemaining:   timeRemaining,
		ActiveChunks:    activeChunks,
		CompletedChunks: completedChunks,
		TotalChunks:     totalChunks,
		Error:           errorMsg,
		LastUpdated:     time.Now(),
	}
}

// SetContext sets the download context and cancel function
func (d *Download) SetContext(ctx context.Context, cancelFunc context.CancelFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ctx = ctx
	d.cancelFunc = cancelFunc
}

// Context returns the download context
func (d *Download) Context() context.Context {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.ctx
}

// CancelFunc returns the cancel function
func (d *Download) CancelFunc() context.CancelFunc {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.cancelFunc
}

func (d *Download) SetProgressFunction() {
	for _, c := range d.Chunks {
		c.SetProgressFunc(d.AddProgress)
	}
}

// GetProgressChannel returns the progress channel
func (d *Download) GetProgressChannel() chan common.Progress {
	return d.progressCh
}

// AddProgress adds progress to the download
func (d *Download) AddProgress(bytes int64) {
	atomic.AddInt64(&d.Downloaded, bytes)

	if d.speedCalculator != nil {
		d.speedCalculator.AddBytes(bytes)
	}

	select {
	case d.progressCh <- common.Progress{
		DownloadID:     d.ID,
		BytesCompleted: atomic.LoadInt64(&d.Downloaded),
		TotalBytes:     d.TotalSize,
		Speed:          d.speedCalculator.GetSpeed(),
		Status:         d.Status,
		Timestamp:      time.Now(),
	}:
	default:
	}
}

// PrepareForSerialization prepares the download for storage
func (d *Download) PrepareForSerialization() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Save chunk information for serialization
	d.ChunkInfos = make([]common.ChunkInfo, len(d.Chunks))
	for i, c := range d.Chunks {
		d.ChunkInfos[i] = common.ChunkInfo{
			ID:                 c.ID.String(),
			StartByte:          c.StartByte,
			EndByte:            c.EndByte,
			Downloaded:         c.Downloaded,
			Status:             c.Status,
			RetryCount:         c.RetryCount,
			TempFilePath:       c.TempFilePath,
			SequentialDownload: c.SequentialDownload,
			LastActive:         c.LastActive,
		}
	}

	if d.Error != nil {
		d.ErrorMessage = d.Error.Error()
	}
}

// RestoreFromSerialization restores runtime fields after loading from storage
func (d *Download) RestoreFromSerialization() {
	d.progressCh = make(chan common.Progress, 10)
	d.speedCalculator = NewSpeedCalculator(5)

	if d.ErrorMessage != "" && d.Error == nil {
		d.Error = errors.New(d.ErrorMessage)
	}

	// Note: Chunks need to be recreated by the Engine using ChunkInfos
	// This is handled separately in the Engine.restoreChunks method
}
