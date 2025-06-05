package http

import (
	"bufio"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/logger"
)

const (
	// MinChunkSize is the minimum size of a chunk in bytes (256 KB).
	minChunkSize int64 = 256 * 1024
	// DefaultChunkSize is the default size of a chunk (4 MB).
	defaultChunkSize int64 = 4 * 1024 * 1024
)

// ChunkManager implements ChunkManager for HTTP downloads.
type ChunkManager struct {
	tempDir          string
	defaultChunkSize int64
}

// newChunkManager creates a new chunk manager.
func newChunkManager(downloadID, tempDir string) (*ChunkManager, error) {
	if tempDir == "" {
		defaultTemp := filepath.Join(os.TempDir(), downloadID)
		logger.Debugf("No temp directory specified, using default: %s", defaultTemp)
		tempDir = defaultTemp
	}

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		logger.Warnf("Failed to create primary temp directory %s: %v, trying fallback", tempDir, err)
		tempDir = filepath.Join(os.TempDir(), "tdm-chunks")
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			logger.Errorf("Failed to create fallback temp directory %s: %v", tempDir, err)
			return nil, chunk.ErrChunkTempDirCreate
		}
	}

	return &ChunkManager{
		tempDir:          tempDir,
		defaultChunkSize: defaultChunkSize,
	}, nil
}

// CreateChunks divides an HTTP download into byte-range based chunks.
func (m *ChunkManager) CreateChunks(downloadID uuid.UUID, downloadInfo *common.DownloadInfo, config *common.Config, progressFn func(int64)) ([]*chunk.Chunk, error) {
	logger.Debugf("Creating HTTP chunks for download %s: filesize=%d, supportsRange=%v, maxConnections=%d",
		downloadID, downloadInfo.TotalSize, downloadInfo.SupportsRanges, config.Connections)

	downloadTempDir := filepath.Join(m.tempDir, downloadID.String())
	logger.Debugf("Creating temp directory for chunks: %s", downloadTempDir)

	if err := os.MkdirAll(downloadTempDir, 0o755); err != nil {
		logger.Errorf("Failed to create temp directory %s: %v", downloadTempDir, err)
		return nil, chunk.ErrChunkTempDirCreate
	}

	filesize := downloadInfo.TotalSize
	maxConnections := config.Connections

	// Handle small files or servers that don't support range requests
	if !downloadInfo.SupportsRanges || filesize < minChunkSize {
		logger.Debugf("Creating single chunk for download: supportsRange=%v, fileSize=%d",
			downloadInfo.SupportsRanges, filesize)

		c := chunk.NewChunk(downloadID, 0, filesize-1, progressFn)
		c.TempFilePath = filepath.Join(downloadTempDir, c.ID.String())

		if !downloadInfo.SupportsRanges {
			c.SequentialDownload = true
			logger.Debugf("Server doesn't support range requests, marked as sequential download")
		}

		logger.Debugf("Created single chunk with ID: %s, range: 0-%d", c.ID, filesize-1)
		return []*chunk.Chunk{c}, nil
	}

	// Calculate optimal number of chunks
	numChunks := maxConnections
	chunkSize := filesize / int64(numChunks)
	logger.Debugf("Calculated %d chunks of ~%d bytes each for file size %d",
		numChunks, chunkSize, filesize)

	// Ensure chunk size is at least the minimum size
	if chunkSize < minChunkSize {
		chunkSize = minChunkSize
		numChunks = int(math.Ceil(float64(filesize) / float64(chunkSize)))
		logger.Debugf("Adjusted to %d chunks of %d bytes each (minimum chunk size)",
			numChunks, chunkSize)
	}

	chunks := make([]*chunk.Chunk, 0, numChunks)
	var startByte int64
	for i := range numChunks {
		endByte := startByte + chunkSize - 1
		if i == numChunks-1 || endByte >= filesize-1 {
			endByte = filesize - 1
		}

		c := chunk.NewChunk(downloadID, startByte, endByte, progressFn)
		c.TempFilePath = filepath.Join(downloadTempDir, c.ID.String())
		chunks = append(chunks, c)

		logger.Debugf("Created chunk %d/%d: ID=%s, range=%d-%d, size=%d bytes",
			i+1, numChunks, c.ID, startByte, endByte, c.Size())

		startByte = endByte + 1

		if endByte >= filesize-1 {
			break
		}
	}

	logger.Infof("Created %d HTTP chunks for download %s", len(chunks), downloadID)
	return chunks, nil
}

// MergeChunks combines HTTP chunks by concatenating them in byte order.
func (m *ChunkManager) MergeChunks(chunks []*chunk.Chunk, targetPath string) error {
	if len(chunks) == 0 {
		logger.Warnf("No chunks provided for merging to %s", targetPath)
		return nil
	}

	downloadID := chunks[0].DownloadID
	logger.Infof("Merging %d HTTP chunks for download %s to %s", len(chunks), downloadID, targetPath)

	// Verify all chunks are complete
	for _, c := range chunks {
		if c.Status != common.StatusCompleted {
			logger.Errorf("Cannot merge: c %s is in status %s", c.ID, c.Status)
			return chunk.ErrMergeIncomplete
		}
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(targetPath)
	logger.Debugf("Ensuring target directory exists: %s", targetDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logger.Errorf("Failed to create target directory %s: %v", targetDir, err)
		return chunk.ErrChunkDirCreate
	}

	logger.Debugf("Creating output file: %s", targetPath)
	outFile, err := os.Create(targetPath)
	if err != nil {
		logger.Errorf("Failed to create output file: %v", err)
		return chunk.ErrTargetFileCreate
	}
	defer outFile.Close()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer
	defer bufWriter.Flush()

	logger.Debugf("Sorting %d chunks by start byte", len(chunks))
	sortedChunks := m.sortChunksByStartByte(chunks)

	totalBytes := int64(0)
	for i, c := range sortedChunks {
		logger.Debugf("Processing c %d/%d: %s (range: %d-%d)",
			i+1, len(sortedChunks), c.ID, c.StartByte, c.EndByte)

		logger.Debugf("Opening c file: %s", c.TempFilePath)
		chunkFile, err := os.Open(c.TempFilePath)
		if err != nil {
			logger.Errorf("Failed to open c file %s: %v", c.TempFilePath, err)
			return chunk.ErrChunkFileOpen
		}

		bytesCopied, err := io.Copy(bufWriter, chunkFile)
		logger.Debugf("Copied %d bytes from c %s", bytesCopied, c.ID)
		totalBytes += bytesCopied

		if err != nil {
			chunkFile.Close()
			logger.Errorf("Failed to copy c data: %v", err)
			return chunk.ErrChunkFileCopy
		}

		chunkFile.Close()
		c.Status = common.StatusMerging
		logger.Debugf("Chunk %s merged successfully", c.ID)
	}

	logger.Debugf("Flushing %d bytes to disk", totalBytes)
	if err := bufWriter.Flush(); err != nil {
		logger.Errorf("Failed to flush data to file: %v", err)
		return chunk.ErrChunkFileWrite
	}

	logger.Infof("Successfully merged %d HTTP chunks (%d bytes) to %s",
		len(chunks), totalBytes, targetPath)
	return nil
}

// CleanupChunks removes temporary chunk files for HTTP downloads.
func (m *ChunkManager) CleanupChunks(chunks []*chunk.Chunk) error {
	if len(chunks) == 0 {
		logger.Debugf("No chunks to clean up")
		return nil
	}

	downloadID := chunks[0].DownloadID
	logger.Infof("Cleaning up %d HTTP chunks for download %s", len(chunks), downloadID)

	downloadTempDir := filepath.Join(m.tempDir, downloadID.String())
	logger.Debugf("Chunk directory to clean: %s", downloadTempDir)

	var lastErr error
	removedCount := 0
	for _, c := range chunks {
		logger.Debugf("Removing c file: %s", c.TempFilePath)
		if err := os.Remove(c.TempFilePath); err != nil {
			if os.IsNotExist(err) {
				logger.Debugf("Chunk file already removed: %s", c.TempFilePath)
			} else {
				logger.Warnf("Failed to remove c file %s: %v", c.TempFilePath, err)
				lastErr = chunk.ErrChunkFileRemove
			}
		} else {
			removedCount++
		}
	}

	logger.Debugf("Removed %d/%d c files, now removing directory: %s",
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
		logger.Warnf("HTTP c cleanup completed with errors: %v", lastErr)
	} else {
		logger.Infof("HTTP c cleanup completed successfully for download %s", downloadID)
	}

	return lastErr
}

// sortChunksByStartByte sorts chunks by their start byte position.
func (m *ChunkManager) sortChunksByStartByte(chunks []*chunk.Chunk) []*chunk.Chunk {
	sortedChunks := make([]*chunk.Chunk, len(chunks))
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
