package chunk

import (
	"context"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"

	"github.com/NamanBalaji/tdm/internal/common"

	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/google/uuid"
)

type Chunk struct {
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

// Progress represents a progress update event for the chunk
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

// NewChunk creates a new chunk with specified parameters
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

// Size returns the total size of the chunk in bytes
func (c *Chunk) Size() int64 {
	return c.EndByte - c.StartByte + 1
}

// Download performs the actual download of the chunk data
func (c *Chunk) Download(ctx context.Context) error {
	logger.Debugf("Starting download of chunk %s (range: %d-%d, size: %d bytes)",
		c.ID, c.StartByte, c.EndByte, c.Size())

	c.Status = common.StatusActive
	logger.Debugf("Opening temp file: %s", c.TempFilePath)

	file, err := os.OpenFile(c.TempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		logger.Errorf("Failed to open temp file for chunk %s: %v", c.ID, err)
		return c.handleError(err)
	}
	defer file.Close()

	if c.Downloaded > 0 {
		logger.Debugf("Chunk %s resuming from offset %d", c.ID, c.Downloaded)
		if _, err := file.Seek(c.Downloaded, 0); err != nil {
			logger.Errorf("Failed to seek to position %d in file: %v", c.Downloaded, err)
			return c.handleError(err)
		}
	}

	logger.Debugf("Starting download loop for chunk %s", c.ID)
	return c.downloadLoop(ctx, file)
}

func (c *Chunk) downloadLoop(ctx context.Context, file *os.File) error {
	buffer := make([]byte, 32*1024)
	bytesRemaining := c.Size() - c.Downloaded

	logger.Debugf("Download loop started for chunk %s, %d bytes remaining", c.ID, bytesRemaining)

	for bytesRemaining > 0 {
		select {
		case <-ctx.Done():
			logger.Debugf("Download cancelled for chunk %s", c.ID)
			c.Status = common.StatusPaused
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
					return c.handleError(writeErr)
				}

				c.Downloaded += int64(n)
				bytesRemaining -= int64(n)
				c.progressFn(int64(n))
				c.LastActive = time.Now()
			}

			if err != nil {
				if err == io.EOF {
					logger.Debugf("Chunk %s download completed (EOF received)", c.ID)
					c.Status = common.StatusCompleted
					return nil
				}
				logger.Errorf("Errorf reading data for chunk %s: %v", c.ID, err)
				return c.handleError(err)
			}
		}
	}

	logger.Debugf("Chunk %s download completed successfully", c.ID)
	c.Status = common.StatusCompleted
	return nil
}

func (c *Chunk) handleError(err error) error {
	logger.Errorf("Chunk %s download failed: %v", c.ID, err)
	c.Status = common.StatusFailed
	c.Error = err
	return c.Error
}

// Reset prepares a Chunk so it can be retried
func (c *Chunk) Reset() {
	logger.Debugf("Resetting chunk %s for retry (attempt #%d)", c.ID, c.RetryCount+1)

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

// VerifyIntegrity checks if the Chunk is completely downloaded and valid
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

// SetProgressFunc is a helper method for setting the progress function on a chunk
func (c *Chunk) SetProgressFunc(progressFn func(int64)) {
	logger.Debugf("Setting progress function for chunk %s", c.ID)
	c.progressFn = progressFn
}
