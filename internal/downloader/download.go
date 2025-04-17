package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/protocol"
)

// Download represents a file download task.
type Download struct {
	ID           uuid.UUID          `json:"id"`
	URL          string             `json:"url"`
	Filename     string             `json:"filename"`
	Config       *common.Config     `json:"config"`
	Status       common.Status      `json:"status"`
	TotalSize    int64              `json:"total_size"`
	Downloaded   int64              `json:"downloaded"` // Downloaded bytes so far
	StartTime    time.Time          `json:"start_time,omitempty"`
	EndTime      time.Time          `json:"end_time,omitempty"`
	ChunkInfos   []common.ChunkInfo `json:"chunk_infos"` // Chunk data for serialization
	Chunks       []*chunk.Chunk     `json:"-"`
	TotalChunks  int32              `json:"total_chunks"`
	ErrorMessage string             `json:"error_message,omitempty"` // For persistent storage

	// runtime fields
	error           error
	mu              sync.RWMutex
	ctx             context.Context
	cancelFunc      context.CancelFunc
	chunkManager    *chunk.Manager
	protocolHandler protocol.Protocol
	done            chan struct{}
	progressCh      chan common.Progress
	saveStateChan   chan *Download
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

// GetTotalChunks returns the total number of chunks.
func (d *Download) GetTotalChunks() int {
	return int(atomic.LoadInt32(&d.TotalChunks))
}

func (d *Download) SetTotalChunks(total int) {
	atomic.StoreInt32(&d.TotalChunks, int32(total))
}

// addChunks adds chunks to the Download.
func (d *Download) addChunks(chunks ...*chunk.Chunk) {
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

// GetTotalSize returns the total size of the Download.
func (d *Download) GetTotalSize() int64 {
	return atomic.LoadInt64(&d.TotalSize)
}

// NewDownload creates a new Download instance.
func NewDownload(ctx context.Context, url string, proto *protocol.Handler, config *common.Config, saveStateChan chan *Download) (*Download, error) {
	id := uuid.New()
	logger.Infof("Creating new download: id=%s, url=%s", id, url)

	handler, err := proto.GetHandler(url)
	if err != nil {
		return nil, err
	}

	info, err := handler.Initialize(ctx, url, config)
	if err != nil {
		return nil, fmt.Errorf("error initializing handler: %w", err)
	}

	if err := os.MkdirAll(config.Directory, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory for download: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	download := &Download{
		ID:              id,
		URL:             url,
		protocolHandler: handler,
		Filename:        info.Filename,
		TotalSize:       info.TotalSize,
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
		saveStateChan:   saveStateChan,
	}

	chunkManager, err := chunk.NewManager(id.String(), config.TempDir)
	if err != nil {
		return nil, err
	}
	chunks, err := chunkManager.CreateChunks(id, info.TotalSize, info.SupportsRanges, config.Connections, download.addProgress)

	download.addChunks(chunks...)
	download.chunkManager = chunkManager

	return download, nil
}

// GetStats returns current download statistics.
func (d *Download) GetStats() Stats {
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
	if d.error != nil {
		errorMsg = d.error.Error()
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

	d.Downloaded = atomic.LoadInt64(&d.Downloaded)
	d.Status = d.GetStatus()

	// Save chunk information for serialization
	d.ChunkInfos = make([]common.ChunkInfo, len(d.Chunks))
	for i, c := range d.Chunks {
		d.ChunkInfos[i] = common.ChunkInfo{
			ID:                 c.ID.String(),
			StartByte:          c.GetStartByte(),
			EndByte:            c.GetEndByte(),
			Downloaded:         c.GetDownloaded(),
			Status:             c.GetStatus(),
			RetryCount:         c.GetRetryCount(),
			TempFilePath:       c.TempFilePath,
			SequentialDownload: c.SequentialDownload,
			LastActive:         c.LastActive,
		}

		logger.Debugf("Serialized chunk %d/%d: id=%s, range=%d-%d, downloaded=%d, status=%s",
			i+1, len(d.Chunks), c.ID, c.StartByte, c.EndByte, c.Downloaded, c.Status)
	}

	if d.error != nil {
		d.ErrorMessage = d.error.Error()
		logger.Debugf("Serialized error message: %s", d.ErrorMessage)
	}

	logger.Debugf("Download %s prepared for serialization with %d chunks", d.ID, len(d.ChunkInfos))
}

// RestoreFromSerialization restores runtime fields after loading from storage.
func (d *Download) RestoreFromSerialization(ctx context.Context, proto *protocol.Handler, saveStateChan chan *Download) error {
	logger.Debugf("Restoring download %s from serialization", d.ID)
	protocolHandler, err := proto.GetHandler(d.URL)
	if err != nil {
		return err
	}
	d.protocolHandler = protocolHandler

	chunkManager, err := chunk.NewManager(d.ID.String(), d.Config.TempDir)
	if err != nil {
		return err
	}
	d.chunkManager = chunkManager

	d.ctx, d.cancelFunc = context.WithCancel(ctx)

	d.progressCh = make(chan common.Progress, 10)
	d.saveStateChan = saveStateChan
	d.done = make(chan struct{})
	d.speedCalculator = NewSpeedCalculator(5)
	d.SetTotalChunks(len(d.ChunkInfos))

	if d.ErrorMessage != "" && d.error == nil {
		d.error = errors.New(d.ErrorMessage)
		logger.Debugf("Restored error message: %s", d.ErrorMessage)
	}

	err = d.restoreChunks()
	if err != nil {
		return fmt.Errorf("failed to restore chunks: %w", err)
	}

	savingNeeded := false
	if d.GetStatus() == common.StatusActive {
		d.SetStatus(common.StatusPaused)
		savingNeeded = true
	}
	if d.GetStatus() == common.StatusCompleted {
		outputPath := filepath.Join(d.Config.Directory, d.Filename)
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			d.SetStatus(common.StatusFailed)
			d.error = errors.New("output file missing")
			savingNeeded = true
		}
	}

	if savingNeeded {
		d.saveStateChan <- d
	}

	logger.Debugf("Download %s restored from serialization (chunks will be restored separately)", d.ID)

	return nil
}

// restoreChunks restores the chunks from the serialized Download data.
func (d *Download) restoreChunks() error {
	if len(d.ChunkInfos) == 0 {
		logger.Debugf("No chunk information available for download %s", d.ID)
		return nil
	}

	chunks := make([]*chunk.Chunk, d.TotalChunks)
	for i, info := range d.ChunkInfos {
		chunkID, err := uuid.Parse(info.ID)
		if err != nil {
			logger.Errorf("Failed to parse chunk ID %s: %v", info.ID, err)
			return err
		}

		newChunk := &chunk.Chunk{
			ID:                 chunkID,
			DownloadID:         d.ID,
			StartByte:          info.StartByte,
			EndByte:            info.EndByte,
			Downloaded:         info.Downloaded,
			Status:             info.Status,
			RetryCount:         info.RetryCount,
			TempFilePath:       info.TempFilePath,
			SequentialDownload: info.SequentialDownload,
			LastActive:         info.LastActive,
		}

		if _, err := os.Stat(newChunk.TempFilePath); os.IsNotExist(err) {
			logger.Debugf("Chunk file %s does not exist, checking directory", newChunk.TempFilePath)
			chunkDir := filepath.Dir(newChunk.TempFilePath)
			if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
				logger.Debugf("Creating chunk directory: %s", chunkDir)
				if err := os.MkdirAll(chunkDir, 0o755); err != nil {
					logger.Warnf("Failed to create chunk directory %s: %v", chunkDir, err)
				}
			}

			if newChunk.GetStatus() == common.StatusCompleted {
				logger.Debugf("Resetting completed chunk %s as file is missing", newChunk.ID)
				newChunk.SetStatus(common.StatusPending)
				newChunk.Downloaded = 0
			}
		}

		chunks[i] = newChunk
		logger.Debugf("Restored chunk %s with status %s, range: %d-%d, downloaded: %d",
			newChunk.ID, newChunk.GetStatus(), newChunk.GetStartByte(), newChunk.GetEndByte(), newChunk.GetDownloaded())
	}

	d.addChunks(chunks...)
	d.setProgressFunction()
	logger.Debugf("All chunks restored for download %s", d.ID)

	return nil
}

// AddProgress adds progress to the download.
func (d *Download) addProgress(bytes int64) {
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

// setProgressFunction sets the progress function for each chunk.
func (d *Download) setProgressFunction() {
	logger.Debugf("Setting progress function for %d chunks in download %s", len(d.Chunks), d.ID)

	for i, c := range d.Chunks {
		logger.Debugf("Setting progress function for chunk %d/%d (%s) in download %s",
			i+1, len(d.Chunks), c.ID, d.ID)
		c.SetProgressFunc(d.addProgress)
	}
}
