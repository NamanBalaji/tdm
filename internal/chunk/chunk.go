package chunk

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/logger"
)

type Chunk struct {
	mu           sync.RWMutex
	ID           uuid.UUID             `json:"ID"`           // Unique identifier
	DownloadID   uuid.UUID             `json:"DownloadID"`   // ID of the parent download
	StartByte    int64                 `json:"StartByte"`    // Starting byte position in the original file
	EndByte      int64                 `json:"EndByte"`      // Ending byte position in the original file
	Downloaded   int64                 `json:"Downloaded"`   // Number of bytes downloaded (atomic)
	Status       common.Status         `json:"Status"`       // Current status
	TempFilePath string                `json:"TempFilePath"` // Path to temporary file for this chunk
	Error        error                 `json:"_"`            // Last error encountered
	Connection   connection.Connection `json:"-"`
	RetryCount   int                   `json:"RetryCount"`           // Number of times this chunk has been retried
	LastActive   time.Time             `json:"LastActive,omitempty"` // Last time data was received
	progressFn   func(int64)
	// Special flags
	SequentialDownload bool `json:"SequentialDownload"` // True if server doesn't support ranges and we need sequential download
}

// Progress represents a progress update event for the chunk.
type Progress struct {
	ChunkID            uuid.UUID
	BytesCompleted     int64
	TotalBytes         int64
	Speed              int64
	Status             common.Status
	Error              error
	Timestamp          time.Time
	SequentialDownload bool // True if server doesn't support ranges and we need sequential download
}

// NewChunk creates a new chunk with specified parameters.
func NewChunk(downloadID uuid.UUID, startByte, endByte int64, progressFn func(int64)) *Chunk {
	chunk := &Chunk{
		ID:         uuid.New(),
		DownloadID: downloadID,
		StartByte:  startByte,
		EndByte:    endByte,
		Status:     common.StatusPending,
		progressFn: progressFn,
	}

	logger.Debugf("Created new chunk %s for download %s (range: %d-%d, size: %d bytes)",
		chunk.ID, downloadID, startByte, endByte, chunk.Size())

	return chunk
}

// GetStatus returns the status of the chunk safely.
func (c *Chunk) GetStatus() common.Status {
	return common.Status(atomic.LoadInt32((*int32)(&c.Status)))
}

// SetStatus safely sets the status of the chunk.
func (c *Chunk) SetStatus(status common.Status) {
	atomic.StoreInt32((*int32)(&c.Status), int32(status))
}

// GetStartByte returns the starting byte position safely.
func (c *Chunk) GetStartByte() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.StartByte
}

// SetStartByte safely sets the starting byte position.
func (c *Chunk) SetStartByte(value int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.StartByte = value
}

// GetEndByte returns the ending byte position safely.
func (c *Chunk) GetEndByte() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.EndByte
}

// SetEndByte safely sets the ending byte position.
func (c *Chunk) SetEndByte(value int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.EndByte = value
}

// GetDownloaded returns the number of bytes downloaded safely.
func (c *Chunk) GetDownloaded() int64 {
	return atomic.LoadInt64(&c.Downloaded)
}

// SetDownloaded safely sets the number of bytes downloaded.
func (c *Chunk) SetDownloaded(value int64) {
	atomic.StoreInt64(&c.Downloaded, value)
}

// AddDownloaded atomically adds to the downloaded bytes counter.
func (c *Chunk) AddDownloaded(value int64) int64 {
	return atomic.AddInt64(&c.Downloaded, value)
}

// Size returns the total size of the chunk in bytes.
func (c *Chunk) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.EndByte - c.StartByte + 1
}

// based on what has been downloaded so far.
func (c *Chunk) GetCurrentByteRange() (start int64, end int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	currentStart := c.StartByte + atomic.LoadInt64(&c.Downloaded)
	return currentStart, c.EndByte
}

// CanDownload checks if the chunk is in a state where it can be downloaded.
func (c *Chunk) CanDownload() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Status == common.StatusPending || c.Status == common.StatusPaused
}

// GetRetryCount retrieves the retry count safely.
func (c *Chunk) GetRetryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RetryCount
}

// SetRetryCount safely sets the retry count.
func (c *Chunk) SetRetryCount(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.RetryCount = value
}

// SetError sets the error for the chunk.
func (c *Chunk) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Error = err
}

// SetConnection sets the connection for the chunk.
func (c *Chunk) SetConnection(conn connection.Connection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Connection = conn
}

// Download performs the actual download of the chunk data.
func (c *Chunk) Download(ctx context.Context) error {
	c.mu.Lock()
	c.Status = common.StatusActive
	tempFilePath := c.TempFilePath
	isSequential := c.SequentialDownload

	downloaded := atomic.LoadInt64(&c.Downloaded)

	c.mu.Unlock()

	logger.Debugf("Starting download of chunk %s (range: %d-%d, size: %d bytes, already downloaded: %d bytes)",
		c.ID, c.StartByte, c.EndByte, c.Size(), downloaded)

	logger.Debugf("Opening temp file: %s", tempFilePath)
	file, err := os.OpenFile(tempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		logger.Errorf("Failed to open temp file for chunk %s: %v", c.ID, err)
		return c.handleError(ErrChunkFileOpen)
	}
	defer file.Close()

	if downloaded > 0 && !isSequential {
		logger.Debugf("Chunk %s resuming from offset %d", c.ID, downloaded)
		if _, err := file.Seek(downloaded, 0); err != nil {
			logger.Errorf("Failed to seek to position %d in file: %v", downloaded, err)
			return c.handleError(ErrFileSeek)
		}
	} else {
		logger.Debugf("Chunk %s starting from beginning", c.ID)
		if _, err := file.Seek(0, 0); err != nil {
			logger.Errorf("Failed to seek to beginning of file: %v", err)
			return c.handleError(ErrFileSeek)
		}
	}

	logger.Debugf("Starting download loop for chunk %s", c.ID)
	return c.downloadLoop(ctx, file)
}

func (c *Chunk) downloadLoop(ctx context.Context, file *os.File) error {
	buffer := make([]byte, 32*1024)
	c.mu.RLock()
	totalSize := c.EndByte - c.StartByte + 1
	c.mu.RUnlock()

	downloaded := atomic.LoadInt64(&c.Downloaded)
	bytesRemaining := totalSize - downloaded

	logger.Debugf("Download loop started for chunk %s, %d bytes remaining", c.ID, bytesRemaining)

	for bytesRemaining > 0 {
		select {
		case <-ctx.Done():
			logger.Debugf("Download cancelled for chunk %s", c.ID)
			c.SetStatus(common.StatusPaused)
			return ctx.Err()
		default:
			n, err := c.Connection.Read(ctx, buffer)

			if n > 0 {
				if bytesToWrite := int64(n); bytesToWrite > bytesRemaining {
					logger.Debugf("Chunk %s received more bytes than needed, truncating", c.ID)
					n = int(bytesRemaining)
				}

				if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
					logger.Errorf("Failed to write to file for chunk %s: %v", c.ID, writeErr)
					return c.handleError(ErrFileWrite)
				}

				newDownloaded := atomic.AddInt64(&c.Downloaded, int64(n))
				bytesRemaining = totalSize - newDownloaded

				c.mu.Lock()
				c.LastActive = time.Now()
				c.mu.Unlock()
				c.progressFn(int64(n))
			}

			if err != nil {
				if err == io.EOF {
					logger.Debugf("Chunk %s download completed (EOF received)", c.ID)
					c.SetStatus(common.StatusCompleted)
					return nil
				}
				logger.Errorf("Errorf reading data for chunk %s: %v", c.ID, err)
				return c.handleError(err)
			}
		}
	}

	logger.Debugf("Chunk %s download completed successfully", c.ID)
	c.SetStatus(common.StatusCompleted)
	return nil
}

func (c *Chunk) handleError(err error) error {
	logger.Errorf("Chunk %s download failed: %v", c.ID, err)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Status = common.StatusFailed
	c.Error = err

	return c.Error
}

// Reset prepares a Chunk so it can be retried.
func (c *Chunk) Reset() {
	logger.Debugf("Resetting chunk %s for retry (attempt #%d)", c.ID, c.RetryCount+1)
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Connection != nil {
		logger.Debugf("Closing connection for chunk %s", c.ID)
		c.Connection.Close()
	}

	c.Status = common.StatusPending
	c.Error = nil
	c.Connection = nil
	c.RetryCount++
	c.LastActive = time.Now()

	logger.Debugf("Chunk %s reset complete, new status: %s", c.ID, c.Status)
}

// VerifyIntegrity checks if the Chunk is completely downloaded and valid.
func (c *Chunk) VerifyIntegrity() bool {
	logger.Debugf("Verifying integrity of chunk %s", c.ID)

	downloadedBytes := atomic.LoadInt64(&c.Downloaded)
	expectedSize := c.Size()

	if downloadedBytes != expectedSize {
		logger.Warnf("Integrity check failed for chunk %s: downloaded=%d, expected=%d",
			c.ID, downloadedBytes, expectedSize)
		return false
	}

	logger.Debugf("Integrity check passed for chunk %s", c.ID)
	return true
}

// SetProgressFunc is a helper method for setting the progress function on a chunk.
func (c *Chunk) SetProgressFunc(progressFn func(int64)) {
	logger.Debugf("Setting progress function for chunk %s", c.ID)
	c.progressFn = progressFn
}
