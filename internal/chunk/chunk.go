package chunk

import (
	"context"
	"github.com/NamanBalaji/tdm/internal/common"
	"io"
	"os"
	"sync/atomic"
	"time"

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

	// For progress reporting
	progressCh         chan<- Progress
	lastProgressUpdate time.Time `json:"-"`
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
	return &Chunk{
		ID:         uuid.New(),
		DownloadID: downloadID,
		StartByte:  startByte,
		EndByte:    endByte,
		Status:     common.StatusPending,
		progressFn: progressFn,
	}
}

// Size returns the total size of the chunk in bytes
func (c *Chunk) Size() int64 {
	return c.EndByte - c.StartByte + 1
}

// Download performs the actual download of the chunk data
func (c *Chunk) Download(ctx context.Context) error {
	c.Status = common.StatusActive
	file, err := os.OpenFile(c.TempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return c.handleError(err)
	}
	defer file.Close()

	if c.Downloaded > 0 {
		if _, err := file.Seek(c.Downloaded, 0); err != nil {
			return c.handleError(err)
		}
	}

	return c.downloadLoop(ctx, file)
}

func (c *Chunk) downloadLoop(ctx context.Context, file *os.File) error {
	buffer := make([]byte, 32*1024)

	for {
		select {
		case <-ctx.Done():
			c.Status = common.StatusPaused
			return ctx.Err()
		default:
			n, err := c.Connection.Read(buffer)

			c.LastActive = time.Now()

			if n > 0 {
				if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
					return c.handleError(writeErr)
				}
				c.Downloaded += int64(n)
				c.progressFn(int64(n))
			}

			if err != nil {
				if err == io.EOF {
					c.Status = common.StatusCompleted
					return nil
				}

				return c.handleError(err)
			}
		}
	}
}

func (c *Chunk) handleError(err error) error {
	c.Status = common.StatusFailed
	c.Error = err
	return c.Error
}

// Reset prepares a Chunk so it can be retried
func (c *Chunk) Reset() {
	c.Status = common.StatusPending
	c.Error = nil
	c.Connection = nil
	c.RetryCount++
	c.LastActive = time.Now()
}

// VerifyIntegrity checks if the Chunk is completely downloaded and valid
func (c *Chunk) VerifyIntegrity() bool {
	// also check for checksum later
	if atomic.LoadInt64(&c.Downloaded) != c.Size() {
		return false
	}
	return true
}
