package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/status"
)

var (
	ErrChunkFileOpenFailed  = errors.New("failed to open chunk file")
	ErrChunkFileWriteFailed = errors.New("failed to write to chunk file")
	ErrChunkFileSeekFailed  = errors.New("failed to seek in chunk file")
)

type Chunk struct {
	mu           sync.RWMutex
	ID           uuid.UUID     `json:"id"`
	StartByte    int64         `json:"startByte"`
	EndByte      int64         `json:"endByte"`
	Downloaded   int64         `json:"downloaded"`
	Status       status.Status `json:"status"`
	TempFilePath string        `json:"tempFilePath"`
	RetryCount   int32         `json:"retryCount"`
}

func newChunk(start, end int64, path string) *Chunk {
	id := uuid.New()
	path = filepath.Join(path, id.String())

	return &Chunk{
		ID:           id,
		StartByte:    start,
		EndByte:      end,
		Status:       status.Pending,
		TempFilePath: path,
	}
}

func (c *Chunk) getStatus() status.Status {
	return atomic.LoadInt32(&c.Status)
}

func (c *Chunk) setStatus(status status.Status) {
	atomic.StoreInt32(&c.Status, status)
}

func (c *Chunk) getDownloaded() int64 {
	return atomic.LoadInt64(&c.Downloaded)
}

func (c *Chunk) updateDownloaded(downloaded int64) int64 {
	return atomic.AddInt64(&c.Downloaded, downloaded)
}

func (c *Chunk) getTotalSize() int64 {
	return atomic.LoadInt64(&c.EndByte) - atomic.LoadInt64(&c.StartByte) + 1
}

func (c *Chunk) getRetryCount() int32 {
	return atomic.LoadInt32(&c.RetryCount)
}

func (c *Chunk) setRetryCount(count int32) {
	atomic.StoreInt32(&c.RetryCount, count)
}

func (c *Chunk) getStartByte() int64 {
	return atomic.LoadInt64(&c.StartByte)
}

func (c *Chunk) getEndByte() int64 {
	return atomic.LoadInt64(&c.EndByte)
}

func (c *Chunk) getTempFilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.TempFilePath
}

func (c *Chunk) Download(ctx context.Context, conn *connection, isSequential bool) error {
	c.setStatus(status.Active)

	file, err := os.OpenFile(c.TempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Println(c.TempFilePath)
		c.setStatus(status.Failed)

		return ErrChunkFileOpenFailed
	}
	defer file.Close()

	if c.getDownloaded() > 0 && !isSequential {
		if _, err := file.Seek(c.getDownloaded(), 0); err != nil {
			c.setStatus(status.Failed)
			return ErrChunkFileSeekFailed
		}
	} else {
		if _, err := file.Seek(0, 0); err != nil {
			c.setStatus(status.Failed)
			return ErrChunkFileSeekFailed
		}
	}

	return c.downloadLoop(ctx, conn, file)
}

func (c *Chunk) downloadLoop(ctx context.Context, conn *connection, file *os.File) error {
	buffer := make([]byte, 32*1024)
	totalSize := c.getTotalSize()
	bytesRemaining := totalSize - c.getDownloaded()

	for bytesRemaining > 0 {
		select {
		case <-ctx.Done():
			c.setStatus(status.Cancelled)
			return ctx.Err()
		default:
			n, err := conn.Read(ctx, buffer)
			if n > 0 {
				if bytesToWrite := int64(n); bytesToWrite > bytesRemaining {
					n = int(bytesRemaining) // Adjust n to not exceed remaining bytes
				}

				if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
					c.setStatus(status.Failed)
					return ErrChunkFileWriteFailed
				}

				newDownloaded := c.updateDownloaded(int64(n))
				bytesRemaining = totalSize - newDownloaded
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					c.setStatus(status.Completed)
					return nil
				} else {
					c.setStatus(status.Failed)
					return err
				}
			}
		}
	}

	c.setStatus(status.Completed)

	return nil
}
