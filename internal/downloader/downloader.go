package downloader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/logger"
)

// Download represents a file download task.
type Download struct {
	ID       uuid.UUID `json:"id"`
	URL      string    `json:"url"`
	Filename string    `json:"filename"`

	Config    *Config       `json:"config"`
	Status    common.Status `json:"status"`
	TotalSize int64         `json:"total_size"`

	Downloaded int64     `json:"downloaded"` // Downloaded bytes so far
	StartTime  time.Time `json:"start_time,omitempty"`
	EndTime    time.Time `json:"end_time,omitempty"`

	ChunkInfos  []common.ChunkInfo `json:"chunk_infos"` // Chunk data for serialization
	Chunks      []*chunk.Chunk     `json:"-"`
	TotalChunks int32              `json:"total_chunks"`

	ErrorMessage string `json:"error_message,omitempty"` // For persistent storage
	Error        error  `json:"-"`                       // Runtime only

	mu              sync.RWMutex
	ctx             context.Context
	cancelFunc      context.CancelFunc
	done            chan struct{}
	progressCh      chan common.Progress
	speedCalculator *SpeedCalculator
}

// SetStatus sets the Status of a Download.
func (d *Download) SetStatus(status common.Status) {
	atomic.StoreInt32((*int32)(&d.Status), int32(status))
}

// GetStatus returns the current Status of the Download.
func (d *Download) GetStatus() common.Status {
	return common.Status(atomic.LoadInt32((*int32)(&d.Status)))
}

// SetError sets the Error and ErrorMessage for the Download.
func (d *Download) SetError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Error = err
	d.ErrorMessage = ""
	if err != nil {
		d.ErrorMessage = err.Error()
	}
}

// GetError returns the current Error of the Download.
func (d *Download) GetError() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Error
}

// GetChunks returns the current Chunks of the Download.
func (d *Download) GetChunks() []*chunk.Chunk {
	d.mu.RLock()
	defer d.mu.RUnlock()

	chunks := make([]*chunk.Chunk, len(d.Chunks))
	copy(chunks, d.Chunks)
	return chunks
}

// GetTotalChunks returns the total number of chunks.
func (d *Download) GetTotalChunks() int {
	return int(atomic.LoadInt32(&d.TotalChunks))
}

func (d *Download) SetTotalChunks(total int) {
	atomic.StoreInt32(&d.TotalChunks, int32(total))
}

// AddChunks adds chunks to the Download.
func (d *Download) AddChunks(chunks ...*chunk.Chunk) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, c := range chunks {
		d.Chunks = append(d.Chunks, c)
		logger.Debugf("Added chunk %s to download %s", c.ID, d.ID)
	}
	d.SetTotalChunks(len(d.Chunks))
}

// GetDownloaded returns the number of bytes downloaded.
func (d *Download) GetDownloaded() int64 {
	return atomic.LoadInt64(&d.Downloaded)
}

// SetDownloaded sets the number of bytes downloaded.
func (d *Download) SetDownloaded(bytes int64) {
	atomic.StoreInt64(&d.Downloaded, bytes)
}

// GetTotalSize returns the total size of the Download.
func (d *Download) GetTotalSize() int64 {
	return atomic.LoadInt64(&d.TotalSize)
}

// SetTotalSize sets the total size of the Download.
func (d *Download) SetTotalSize(bytes int64) {
	atomic.StoreInt64(&d.TotalSize, bytes)
}

// Context returns the download context.
func (d *Download) Context() context.Context {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.ctx
}

// SetContextKey sets a key-value pair in the Download context.
func (d *Download) SetContextKey(key string, value interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ctx == nil {
		logger.Debugf("Context is nil, creating new context for download %s", d.ID)
		d.ctx = context.Background()
	}

	d.ctx = context.WithValue(d.ctx, key, value)
	logger.Debugf("Set context key %s for download %s", key, d.ID)
}

// GetContextKey retrieves a value from the Download context.
func (d *Download) GetContextKey(key string) interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.ctx == nil {
		logger.Debugf("Context is nil for download %s", d.ID)
		return nil
	}

	value := d.ctx.Value(key)
	logger.Debugf("Got context key %s for download %s: %v", key, d.ID, value)
	return value
}

// CancelFunc returns the cancel function.
func (d *Download) CancelFunc() context.CancelFunc {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.cancelFunc
}

// WaitForDone waits for the download to finish.
func (d *Download) WaitForDone() {
	<-d.done
}

// Done signals that the download is done.
func (d *Download) Done() {
	close(d.done)
}

// PrepareResume prepares the download for resuming.
func (d *Download) PrepareResume(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.done = make(chan struct{})
	d.ctx, d.cancelFunc = context.WithCancel(ctx)
}

// SetProgressFunction sets the progress function for each chunk.
func (d *Download) SetProgressFunction() {
	logger.Debugf("Setting progress function for %d chunks in download %s", len(d.Chunks), d.ID)

	for i, c := range d.Chunks {
		logger.Debugf("Setting progress function for chunk %d/%d (%s) in download %s",
			i+1, len(d.Chunks), c.ID, d.ID)
		c.SetProgressFunc(d.AddProgress)
	}
}

// GetProgressChannel returns the progress channel.
func (d *Download) GetProgressChannel() chan common.Progress {
	return d.progressCh
}

// AddProgress adds progress to the download.
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
		Status:         d.GetStatus(),
		Timestamp:      time.Now(),
	}:
	default:
		// Channel full, skip this update
	}
}

// NewDownload creates a new Download instance.
func NewDownload(ctx context.Context, url, filename string, totalSize int64, config *Config) *Download {
	id := uuid.New()
	logger.Infof("Creating new download: id=%s, url=%s, filename=%s", id, url, filename)

	ctx, cancel := context.WithCancel(ctx)

	download := &Download{
		ID:              id,
		URL:             url,
		Filename:        filename,
		TotalSize:       totalSize,
		Config:          config,
		Status:          common.StatusPending,
		ChunkInfos:      make([]common.ChunkInfo, 0),
		Chunks:          make([]*chunk.Chunk, 0),
		progressCh:      make(chan common.Progress, 10),
		done:            make(chan struct{}),
		speedCalculator: NewSpeedCalculator(5),
		StartTime:       time.Now(),
		ctx:             ctx,
		cancelFunc:      cancel,
	}

	if config != nil {
		logger.Debugf("Download %s configuration: connections=%d, maxRetries=%d, retryDelay=%v",
			id, config.Connections, config.MaxRetries, config.RetryDelay)
	}

	logger.Debugf("Download %s created with status: %s", id, download.Status)
	return download
}

// GetStats returns current download statistics.
func (d *Download) GetStats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	downloadedBytes := atomic.LoadInt64(&d.Downloaded)
	logger.Debugf("Getting stats for download %s: downloaded=%d bytes", d.ID, downloadedBytes)

	var progress float64
	if d.TotalSize > 0 {
		progress = float64(downloadedBytes) / float64(d.TotalSize) * 100
	}

	var speed int64
	if d.speedCalculator != nil {
		speed = d.speedCalculator.GetSpeed()
	}

	activeChunks := 0
	completedChunks := 0

	for _, c := range d.Chunks {
		switch c.Status {
		case common.StatusActive:
			activeChunks++
		case common.StatusCompleted:
			completedChunks++
		}
	}

	timeElapsed := time.Since(d.StartTime)
	var timeRemaining time.Duration
	if speed > 0 {
		bytesRemaining := d.TotalSize - downloadedBytes
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

	stats := Stats{
		ID:              d.ID,
		Status:          d.GetStatus(),
		TotalSize:       d.GetTotalSize(),
		Downloaded:      downloadedBytes,
		Progress:        progress,
		Speed:           speed,
		TimeElapsed:     timeElapsed,
		TimeRemaining:   timeRemaining,
		ActiveChunks:    activeChunks,
		CompletedChunks: completedChunks,
		TotalChunks:     d.GetTotalChunks(),
		Error:           errorMsg,
		LastUpdated:     time.Now(),
	}

	logger.Debugf("Download %s stats: progress=%.2f%%, speed=%d B/s, active=%d, completed=%d, total=%d",
		d.ID, stats.Progress, stats.Speed, stats.ActiveChunks, stats.CompletedChunks, stats.TotalChunks)

	return stats
}

// PrepareForSerialization prepares the download for storage.
func (d *Download) PrepareForSerialization() {
	logger.Debugf("Preparing download %s for serialization", d.ID)

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

		logger.Debugf("Serialized chunk %d/%d: id=%s, range=%d-%d, downloaded=%d, status=%s",
			i+1, len(d.Chunks), c.ID, c.StartByte, c.EndByte, c.Downloaded, c.Status)
	}

	if d.Error != nil {
		d.ErrorMessage = d.Error.Error()
		logger.Debugf("Serialized error message: %s", d.ErrorMessage)
	}

	logger.Debugf("Download %s prepared for serialization with %d chunks", d.ID, len(d.ChunkInfos))
}

// RestoreFromSerialization restores runtime fields after loading from storage.
func (d *Download) RestoreFromSerialization(ctx context.Context) {
	logger.Debugf("Restoring download %s from serialization", d.ID)

	d.ctx, d.cancelFunc = context.WithCancel(ctx)

	d.progressCh = make(chan common.Progress, 10)
	d.done = make(chan struct{})
	d.speedCalculator = NewSpeedCalculator(5)
	d.SetTotalChunks(len(d.ChunkInfos))

	if d.ErrorMessage != "" && d.Error == nil {
		d.Error = errors.New(d.ErrorMessage)
		logger.Debugf("Restored error message: %s", d.ErrorMessage)
	}

	logger.Debugf("Download %s restored from serialization (chunks will be restored separately)", d.ID)
	// Note: Chunks need to be recreated by the Engine using ChunkInfos
	// This is handled separately in the Engine.restoreChunks method
}
