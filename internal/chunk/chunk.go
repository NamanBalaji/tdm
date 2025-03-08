package chunk

import (
	"context"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/google/uuid"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// Status represents the current state of a chunk
type Status string

const (
	Pending   Status = "pending"   // Initial state
	Active    Status = "active"    // Currently downloading
	Paused    Status = "paused"    // Paused
	Completed Status = "completed" // Successfully completed
	Failed    Status = "failed"    // Failed to download
	Merging   Status = "merging"   // Being merged into final file
	Cancelled Status = "cancelled" // Download cancelled
)

type Chunk struct {
	ID           uuid.UUID // Unique identifier
	DownloadID   uuid.UUID // ID of the parent download
	StartByte    int64     // Starting byte position in the original file
	EndByte      int64     // Ending byte position in the original file
	Downloaded   int64     // Number of bytes downloaded (atomic)
	Status       Status    // Current status
	TempFilePath string    // Path to temporary file for this chunk
	Error        error     // Last error encountered
	Connection   connection.Connection
	RetryCount   int       // Number of times this chunk has been retried
	LastActive   time.Time // Last time data was received

	// Special flags
	SequentialDownload bool // True if server doesn't support ranges and we need sequential download

	// For progress reporting
	progressCh         chan<- Progress
	lastProgressUpdate time.Time
}

// Progress represents a progress update event for the chunk
type Progress struct {
	ChunkID        uuid.UUID
	BytesCompleted int64
	TotalBytes     int64
	Speed          int64
	Status         Status
	Error          error
	Timestamp      time.Time
}

// NewChunk creates a new chunk with specified parameters
func NewChunk(downloadID uuid.UUID, startByte, endByte int64) *Chunk {
	return &Chunk{
		ID:                 uuid.New(),
		DownloadID:         downloadID,
		StartByte:          startByte,
		EndByte:            endByte,
		Status:             Pending,
		progressCh:         make(chan Progress, 100),
		lastProgressUpdate: time.Now(),
	}
}

// Size returns the total size of the chunk in bytes
func (c *Chunk) Size() int64 {
	return c.EndByte - c.StartByte + 1
}

// BytesRemaining returns the number of bytes still to be downloaded
func (c *Chunk) BytesRemaining() int64 {
	return c.Size() - atomic.LoadInt64(&c.Downloaded)
}

// Progress returns the percentage of chunk that has been downloaded
func (c *Chunk) Progress() float64 {
	downloaded := atomic.LoadInt64(&c.Downloaded)
	size := c.Size()
	if size <= 0 {
		return 0
	}
	return float64(downloaded) / float64(size) * 100
}

// Download performs the actual download of the chunk data
func (c *Chunk) Download(ctx context.Context) error {
	c.Status = Active
	file, err := os.OpenFile(c.TempFilePath, os.O_WRONLY|os.O_CREATE, 0644)
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
			c.Status = Paused
			return ctx.Err()
		default:
			n, err := c.Connection.Read(buffer)

			c.LastActive = time.Now()

			if n > 0 {
				if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
					return c.handleError(writeErr)
				}

				// Update progress
				newDownloaded := atomic.AddInt64(&c.Downloaded, int64(n))

				// Send progress update (throttled to avoid flooding)
				if time.Since(c.lastProgressUpdate) > 250*time.Millisecond ||
					newDownloaded >= c.Size() {
					c.lastProgressUpdate = time.Now()

					select {
					case c.progressCh <- Progress{
						ChunkID:        c.ID,
						BytesCompleted: newDownloaded,
						TotalBytes:     c.Size(),
						Timestamp:      time.Now(),
						Status:         c.Status,
					}:
					default:
					}
				}
			}

			if err != nil {
				if err == io.EOF {
					c.Status = Completed
					return nil
				}

				return c.handleError(err)
			}

		}
	}
}

func (c *Chunk) handleError(err error) error {
	c.Status = Failed
	c.Error = err
	return c.Error
}

// Reset prepares a Chunk so it can be retried
func (c *Chunk) Reset() {
	c.Status = Pending
	c.Error = nil
	c.RetryCount++
}

// VerifyIntegrity checks if the Chunk is completely downloaded and valid
func (c *Chunk) VerifyIntegrity() bool {
	// also check for checksum later
	if atomic.LoadInt64(&c.Downloaded) != c.Size() {
		return false
	}
	return true
}
