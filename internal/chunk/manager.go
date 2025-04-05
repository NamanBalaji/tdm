package chunk

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/NamanBalaji/tdm/internal/logger"

	"github.com/NamanBalaji/tdm/internal/common"

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
	logger.Debugf("Creating new chunk manager")

	if tempDir == "" {
		defaultTemp := filepath.Join(os.TempDir(), "tdm-chunks")
		logger.Debugf("No temp directory specified, using default: %s", defaultTemp)
		tempDir = defaultTemp
	}

	logger.Debugf("Using temp directory: %s", tempDir)
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		logger.Warnf("Failed to create primary temp directory %s: %v, trying fallback", tempDir, err)
		tempDir = filepath.Join(os.TempDir(), "tdm-chunks")
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			logger.Errorf("Failed to create fallback temp directory %s: %v", tempDir, err)
			return nil, fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
		}
	}

	manager := &Manager{
		tempDir:          tempDir,
		defaultChunkSize: DefaultChunkSize,
	}

	logger.Debugf("Chunk manager created successfully with defaults: tempDir=%s, chunkSize=%d bytes",
		tempDir, DefaultChunkSize)

	return manager, nil
}

// SetDefaultChunkSize sets the default chunk size
func (m *Manager) SetDefaultChunkSize(size int64) error {
	logger.Debugf("Setting default chunk size to %d bytes", size)

	if size < MinChunkSize || size > MaxChunkSize {
		logger.Errorf("Invalid chunk size %d, must be between %d and %d bytes",
			size, MinChunkSize, MaxChunkSize)
		return ErrInvalidChunkSize
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.defaultChunkSize = size
	logger.Debugf("Default chunk size set to %d bytes", size)
	return nil
}

// CreateChunks divides a download into chunks and returns them
func (m *Manager) CreateChunks(downloadID uuid.UUID, filesize int64, supportsRange bool, maxConnections int, progressFn func(int64)) ([]*Chunk, error) {
	logger.Debugf("Creating chunks for download %s: filesize=%d, supportsRange=%v, maxConnections=%d",
		downloadID, filesize, supportsRange, maxConnections)

	m.mu.Lock()
	defer m.mu.Unlock()

	downloadTempDir := filepath.Join(m.tempDir, downloadID.String())
	logger.Debugf("Creating temp directory for chunks: %s", downloadTempDir)

	if err := os.MkdirAll(downloadTempDir, 0o755); err != nil {
		logger.Errorf("Failed to create temp directory %s: %v", downloadTempDir, err)
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Handle small files or servers that don't support range requests
	if !supportsRange || filesize < MinChunkSize {
		logger.Debugf("Creating single chunk for download: supportsRange=%v, fileSize=%d",
			supportsRange, filesize)

		chunk := NewChunk(downloadID, 0, filesize-1, progressFn)
		chunk.TempFilePath = filepath.Join(downloadTempDir, chunk.ID.String())

		if !supportsRange {
			chunk.SequentialDownload = true
			logger.Debugf("Server doesn't support range requests, marked as sequential download")
		}

		logger.Debugf("Created single chunk with ID: %s, range: 0-%d", chunk.ID, filesize-1)
		return []*Chunk{chunk}, nil
	}

	// Calculate optimal number of chunks
	numChunks := maxConnections
	chunkSize := filesize / int64(numChunks)
	logger.Debugf("Calculated %d chunks of ~%d bytes each for file size %d",
		numChunks, chunkSize, filesize)

	// Ensure chunk size is at least the minimum size
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
		numChunks = int(math.Ceil(float64(filesize) / float64(chunkSize)))
		logger.Debugf("Adjusted to %d chunks of %d bytes each (minimum chunk size)",
			numChunks, chunkSize)
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

		logger.Debugf("Created chunk %d/%d: ID=%s, range=%d-%d, size=%d bytes",
			i+1, numChunks, chunk.ID, startByte, endByte, chunk.Size())

		startByte = endByte + 1

		if endByte >= filesize-1 {
			break
		}
	}

	logger.Infof("Created %d chunks for download %s", len(chunks), downloadID)
	return chunks, nil
}

// MergeChunks combines downloaded chunks into the final file
func (m *Manager) MergeChunks(chunks []*Chunk, targetPath string) error {
	if len(chunks) == 0 {
		logger.Warnf("No chunks provided for merging to %s", targetPath)
		return nil
	}

	downloadID := chunks[0].DownloadID
	logger.Infof("Merging %d chunks for download %s to %s", len(chunks), downloadID, targetPath)

	// Verify all chunks are complete
	for _, chunk := range chunks {
		if chunk.Status != common.StatusCompleted {
			logger.Errorf("Cannot merge: chunk %s is in status %s", chunk.ID, chunk.Status)
			return fmt.Errorf("cannot merge incomplete download: chunk %s is in status %s", chunk.ID, chunk.Status)
		}
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(targetPath)
	logger.Debugf("Ensuring target directory exists: %s", targetDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logger.Errorf("Failed to create target directory %s: %v", targetDir, err)
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	logger.Debugf("Creating output file: %s", targetPath)
	outFile, err := os.Create(targetPath)
	if err != nil {
		logger.Errorf("Failed to create output file: %v", err)
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer
	defer bufWriter.Flush()

	logger.Debugf("Sorting %d chunks by start byte", len(chunks))
	sortedChunks := sortChunksByStartByte(chunks)

	totalBytes := int64(0)
	for i, chunk := range sortedChunks {
		logger.Debugf("Processing chunk %d/%d: %s (range: %d-%d)",
			i+1, len(sortedChunks), chunk.ID, chunk.StartByte, chunk.EndByte)

		logger.Debugf("Opening chunk file: %s", chunk.TempFilePath)
		chunkFile, err := os.Open(chunk.TempFilePath)
		if err != nil {
			logger.Errorf("Failed to open chunk file %s: %v", chunk.TempFilePath, err)
			return fmt.Errorf("failed to open chunk file: %w", err)
		}

		bytesCopied, err := io.Copy(bufWriter, chunkFile)
		logger.Debugf("Copied %d bytes from chunk %s", bytesCopied, chunk.ID)
		totalBytes += bytesCopied

		if err != nil {
			chunkFile.Close()
			logger.Errorf("Failed to copy chunk data: %v", err)
			return fmt.Errorf("failed to copy chunk data: %w", err)
		}

		chunkFile.Close()
		chunk.Status = common.StatusMerging
		logger.Debugf("Chunk %s merged successfully", chunk.ID)
	}

	logger.Debugf("Flushing %d bytes to disk", totalBytes)
	if err := bufWriter.Flush(); err != nil {
		logger.Errorf("Failed to flush data to file: %v", err)
		return fmt.Errorf("failed to flush data to file: %w", err)
	}

	logger.Infof("Successfully merged %d chunks (%d bytes) to %s",
		len(chunks), totalBytes, targetPath)
	return nil
}

// CleanupChunks removes temporary chunk files
func (m *Manager) CleanupChunks(chunks []*Chunk) error {
	if len(chunks) == 0 {
		logger.Debugf("No chunks to clean up")
		return nil
	}

	downloadID := chunks[0].DownloadID
	logger.Infof("Cleaning up %d chunks for download %s", len(chunks), downloadID)

	downloadTempDir := filepath.Join(m.tempDir, downloadID.String())
	logger.Debugf("Chunk directory to clean: %s", downloadTempDir)

	var lastErr error
	removedCount := 0
	for _, chunk := range chunks {
		logger.Debugf("Removing chunk file: %s", chunk.TempFilePath)
		if err := os.Remove(chunk.TempFilePath); err != nil {
			if os.IsNotExist(err) {
				logger.Debugf("Chunk file already removed: %s", chunk.TempFilePath)
			} else {
				logger.Warnf("Failed to remove chunk file %s: %v", chunk.TempFilePath, err)
				lastErr = err
			}
		} else {
			removedCount++
		}
	}

	logger.Debugf("Removed %d/%d chunk files, now removing directory: %s",
		removedCount, len(chunks), downloadTempDir)

	if err := os.Remove(downloadTempDir); err != nil {
		if os.IsNotExist(err) {
			logger.Debugf("Download directory already removed: %s", downloadTempDir)
		} else {
			logger.Warnf("Failed to remove download directory %s: %v", downloadTempDir, err)
			if lastErr == nil {
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		logger.Warnf("Cleanup completed with errors: %v", lastErr)
	} else {
		logger.Infof("Cleanup completed successfully for download %s", downloadID)
	}

	return lastErr
}

// sortChunksByStartByte sorts chunks by their start byte position
func sortChunksByStartByte(chunks []*Chunk) []*Chunk {
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
