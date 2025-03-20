package chunk

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/NamanBalaji/tdm/internal/common"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const (
	// MinChunkSize is the minimum size of a chunk in bytes (256 KB)
	MinChunkSize int64 = 256 * 1024
	// MaxChunkSize is the maximum size of a chunk in bytes (16 MB)
	MaxChunkSize int64 = 16 * 1024 * 1024
	// DefaultChunkSize is the default size of a chunk (4 MB)
	DefaultChunkSize int64 = 4 * 1024 * 1024
)

var (
	// ErrChunkNotFound is returned when a chunk cannot be found
	ErrChunkNotFound = errors.New("chunk not found")
	// ErrInvalidChunkSize is returned when an invalid chunk size is specified
	ErrInvalidChunkSize = errors.New("invalid chunk size")
)

type Manager struct {
	mu               sync.Mutex
	tempDir          string
	defaultChunkSize int64
}

// NewManager creates a new chunk manager
func NewManager(tempDir string) (*Manager, error) {
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "tdm-chunks")
	}

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		// Fall back to system temp dir if creation fails
		tempDir = filepath.Join(os.TempDir(), "tdm-chunks")
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
		}
	}

	return &Manager{
		tempDir:          tempDir,
		defaultChunkSize: DefaultChunkSize,
	}, nil
}

// SetDefaultChunkSize sets the default chunk size
func (m *Manager) SetDefaultChunkSize(size int64) error {
	if size < MinChunkSize || size > MaxChunkSize {
		return ErrInvalidChunkSize
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.defaultChunkSize = size
	return nil
}

// CreateChunks divides a download into chunks and returns them
func (m *Manager) CreateChunks(downloadID uuid.UUID, filesize int64, supportsRange bool, maxConnections int, progressFn func(int64)) ([]*Chunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create download-specific temp directory
	downloadTempDir := filepath.Join(m.tempDir, downloadID.String())
	if err := os.MkdirAll(downloadTempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	if !supportsRange || filesize < MinChunkSize {
		chunk := NewChunk(downloadID, 0, filesize-1, progressFn)
		chunk.TempFilePath = filepath.Join(downloadTempDir, chunk.ID.String())
		if !supportsRange {
			chunk.SequentialDownload = true
		}
		return []*Chunk{chunk}, nil
	}

	// Calculate optimal number of chunks
	numChunks := calculateOptimalChunkCount(filesize, maxConnections)
	chunkSize := filesize / int64(numChunks)

	// Ensure chunk size is at least the minimum size
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
		numChunks = int(math.Ceil(float64(filesize) / float64(chunkSize)))
	}

	chunks := make([]*Chunk, 0, numChunks)
	var startByte int64
	for i := 0; i < numChunks; i++ {
		endByte := startByte + chunkSize - 1
		if i == numChunks-1 || endByte >= filesize-1 {
			endByte = filesize - 1
		}
		chunk := NewChunk(downloadID, startByte, endByte, progressFn)
		chunk.TempFilePath = filepath.Join(downloadTempDir, chunk.ID.String())
		chunks = append(chunks, chunk)

		startByte = endByte + 1

		if endByte >= filesize-1 {
			break
		}
	}

	return chunks, nil
}

// MergeChunks combines downloaded chunks into the final file
func (m *Manager) MergeChunks(chunks []*Chunk, targetPath string) error {
	for _, chunk := range chunks {
		if chunk.Status != common.StatusCompleted {
			return fmt.Errorf("cannot merge incomplete download: chunk %s is in status %s", chunk.ID, chunk.Status)
		}
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer
	defer bufWriter.Flush()

	sortedChunks := sortChunksByStartByte(chunks)
	for _, chunk := range sortedChunks {
		chunkFile, err := os.Open(chunk.TempFilePath)
		if err != nil {
			return fmt.Errorf("failed to open chunk file: %w", err)
		}

		if _, err := io.Copy(bufWriter, chunkFile); err != nil {
			chunkFile.Close()
			return fmt.Errorf("failed to copy chunk data: %w", err)
		}

		chunkFile.Close()
		chunk.Status = common.StatusMerging
	}

	if err := bufWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush data to file: %w", err)
	}

	return nil
}

// CleanupChunks removes temporary chunk files
func (m *Manager) CleanupChunks(chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	downloadID := chunks[0].DownloadID.String()
	downloadTempDir := filepath.Join(m.tempDir, downloadID)

	var lastErr error
	for _, chunk := range chunks {
		if err := os.Remove(chunk.TempFilePath); err != nil {
			lastErr = err
		}
	}

	if err := os.Remove(downloadTempDir); err != nil {
		if lastErr == nil {
			lastErr = err
		}
	}

	return lastErr
}

// calculateOptimalChunkCount calculates the optimal number of chunks based on file size
func calculateOptimalChunkCount(fileSize int64, maxConnections int) int {
	if maxConnections > 0 {
		return maxConnections
	}

	switch {
	case fileSize < 10*1024*1024:
		return 2
	case fileSize < 100*1024*1024:
		return 4
	case fileSize < 1024*1024*1024:
		return 8
	default:
		return 16
	}
}

// sortChunksByStartByte sorts chunks by their start byte position
func sortChunksByStartByte(chunks []*Chunk) []*Chunk {
	// Create a copy to avoid modifying the original slice
	sortedChunks := make([]*Chunk, len(chunks))
	copy(sortedChunks, chunks)

	// Simple insertion sort (chunks are usually already mostly sorted)
	for i := 1; i < len(sortedChunks); i++ {
		j := i
		for j > 0 && sortedChunks[j-1].StartByte > sortedChunks[j].StartByte {
			sortedChunks[j], sortedChunks[j-1] = sortedChunks[j-1], sortedChunks[j]
			j--
		}
	}

	return sortedChunks
}
