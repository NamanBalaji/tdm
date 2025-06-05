package downloader

import (
	"context"
	"encoding/json"
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
	TotalSize    int64              `json:"totalSize"`
	Downloaded   int64              `json:"downloaded"`
	StartTime    time.Time          `json:"startTime,omitempty"`
	EndTime      time.Time          `json:"endTime,omitempty"`
	ChunkInfos   []common.ChunkInfo `json:"chunkInfos"`
	ErrorMessage string             `json:"errorMessage,omitempty"`
	TotalChunks  int32              `json:"totalChunks"`

	mu              sync.RWMutex
	status          common.Status
	downloaded      int64
	totalChunks     int32
	Chunks          []*chunk.Chunk
	error           error
	cancelFunc      context.CancelFunc
	isExternal      int32 // 0 means false, 1 means true
	chunkManager    chunk.Manager
	protocolHandler protocol.Protocol
	done            chan struct{}
	progressCh      chan common.Progress
	saveStateChan   chan<- *Download
	speedCalculator *SpeedCalculator
}

// SetStatus sets the Status of a Download atomically and updates the internal field.
func (d *Download) SetStatus(status common.Status) {
	atomic.StoreInt32((*int32)(&d.status), int32(status))
}

// GetStatus returns the current Status of the Download atomically.
func (d *Download) GetStatus() common.Status {
	return common.Status(atomic.LoadInt32((*int32)(&d.status)))
}

// GetIsExternal returns the isExternal status of the Download atomically.
func (d *Download) GetIsExternal() bool {
	val := atomic.LoadInt32(&d.isExternal)
	return val == 1
}

// SetIsExternal sets the isExternal status of the Download atomically.
func (d *Download) SetIsExternal(isExternal bool) {
	val := int32(0)
	if isExternal {
		val = 1
	}
	atomic.StoreInt32(&d.isExternal, val)
}

// GetTotalChunks returns the total number of chunks atomically.
func (d *Download) GetTotalChunks() int {
	return int(atomic.LoadInt32(&d.totalChunks))
}

// SetTotalChunks sets the total number of chunks atomically (internal helper).
func (d *Download) SetTotalChunks(total int) {
	atomic.StoreInt32(&d.totalChunks, int32(total))
}

// addChunks adds chunks to the Download, protected by a write lock.
func (d *Download) addChunks(chunks ...*chunk.Chunk) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Chunks = append(d.Chunks, chunks...)
	logger.Debugf("Added %d chunks to download %s. Total chunks now: %d", len(chunks), d.ID, len(d.Chunks))

	d.SetTotalChunks(len(d.Chunks))
}

// GetDownloaded returns the number of bytes downloaded atomically.
func (d *Download) GetDownloaded() int64 {
	return atomic.LoadInt64(&d.downloaded)
}

// GetTotalSize returns the total size of the Download (read-only after init, no lock needed).
func (d *Download) GetTotalSize() int64 {
	return d.TotalSize
}

// NewDownload creates a new Download instance.
func NewDownload(ctx context.Context, url string, proto *protocol.Handler, config *common.Config, saveStateChan chan<- *Download) (*Download, error) {
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

	download := &Download{
		ID:              id,
		URL:             url,
		protocolHandler: handler,
		Filename:        info.Filename,
		TotalSize:       info.TotalSize,
		Config:          config,
		status:          common.StatusPending,
		ChunkInfos:      make([]common.ChunkInfo, 0),
		Chunks:          make([]*chunk.Chunk, 0),
		progressCh:      make(chan common.Progress, 10),
		done:            make(chan struct{}),
		speedCalculator: NewSpeedCalculator(5),

		saveStateChan: saveStateChan,
	}
	download.SetStatus(common.StatusPending)

	chunkManager, err := handler.GetChunkManager(id, config.TempDir)
	if err != nil {
		return nil, err
	}
	download.chunkManager = chunkManager

	chunks, err := chunkManager.CreateChunks(id, info, config, download.addProgress)
	if err != nil {
		return nil, fmt.Errorf("failed to create chunks: %w", err)
	}
	download.addChunks(chunks...)
	download.setProgressFunction()

	logger.Infof("New download %s created with %d chunks", id, download.GetTotalChunks())
	return download, nil
}

// GetStats returns current download statistics, protected by a read lock.
func (d *Download) GetStats() Stats {
	d.mu.RLock()

	downloadedBytes := d.GetDownloaded()
	status := d.GetStatus()
	totalChunks := d.GetTotalChunks()
	filename := d.Filename
	totalSize := d.GetTotalSize()
	startTime := d.StartTime
	err := d.error
	errorMsg := d.ErrorMessage
	chunks := d.Chunks

	d.mu.RUnlock()

	logger.Debugf("Getting stats for download %s: downloaded=%d bytes, status=%s", d.ID, downloadedBytes, status)

	var progress float64
	if totalSize > 0 {
		progress = float64(downloadedBytes) / float64(totalSize) * 100
	}

	var speed int64
	if d.speedCalculator != nil {
		speed = d.speedCalculator.GetSpeed()
	}

	activeChunks := 0
	completedChunks := 0

	for _, c := range chunks {
		chunkStatus := c.GetStatus()
		switch chunkStatus {
		case common.StatusActive:
			activeChunks++
		case common.StatusCompleted:
			completedChunks++
		}
	}

	timeElapsed := time.Duration(0)
	if !startTime.IsZero() {
		timeElapsed = time.Since(startTime)
	}

	var timeRemaining time.Duration
	if speed > 0 && status == common.StatusActive {
		bytesRemaining := totalSize - downloadedBytes
		if bytesRemaining > 0 {
			timeRemaining = time.Duration(bytesRemaining/speed) * time.Second
		}
	}

	currentErrorMsg := ""
	if err != nil {
		currentErrorMsg = err.Error()
	} else if errorMsg != "" {
		currentErrorMsg = errorMsg
	}

	stats := Stats{
		ID:              d.ID,
		Filename:        filename,
		Status:          status,
		TotalSize:       totalSize,
		Downloaded:      downloadedBytes,
		Progress:        progress,
		Speed:           speed,
		TimeElapsed:     timeElapsed,
		TimeRemaining:   timeRemaining,
		ActiveChunks:    activeChunks,
		CompletedChunks: completedChunks,
		TotalChunks:     totalChunks,
		Error:           currentErrorMsg,
		LastUpdated:     time.Now(),
	}

	return stats
}

func (d *Download) MarshalJSON() ([]byte, error) {
	d.mu.Lock()

	snapshot := struct {
		ID           uuid.UUID          `json:"id"`
		URL          string             `json:"url"`
		Filename     string             `json:"filename"`
		Config       *common.Config     `json:"config"`
		Status       common.Status      `json:"status"`
		TotalSize    int64              `json:"totalSize"`
		Downloaded   int64              `json:"downloaded"`
		StartTime    time.Time          `json:"startTime,omitempty"`
		EndTime      time.Time          `json:"endTime,omitempty"`
		ChunkInfos   []common.ChunkInfo `json:"chunkInfos"`
		ErrorMessage string             `json:"errorMessage,omitempty"`
		TotalChunks  int32              `json:"totalChunks"`
	}{
		ID:          d.ID,
		URL:         d.URL,
		Filename:    d.Filename,
		Config:      d.Config,
		Status:      d.GetStatus(),
		TotalSize:   d.TotalSize,
		Downloaded:  d.GetDownloaded(),
		StartTime:   d.StartTime,
		EndTime:     d.EndTime,
		TotalChunks: int32(d.GetTotalChunks()),
	}

	if d.error != nil {
		snapshot.ErrorMessage = d.error.Error()
	} else {
		snapshot.ErrorMessage = d.ErrorMessage
	}

	snapshot.ChunkInfos = make([]common.ChunkInfo, 0, len(d.Chunks))
	for _, c := range d.Chunks {
		snapshot.ChunkInfos = append(snapshot.ChunkInfos, common.ChunkInfo{
			ID:                 c.ID.String(),
			StartByte:          c.GetStartByte(),
			EndByte:            c.GetEndByte(),
			Downloaded:         c.GetDownloaded(),
			Status:             c.GetStatus(),
			RetryCount:         c.GetRetryCount(),
			TempFilePath:       c.TempFilePath,
			SequentialDownload: c.SequentialDownload,
			LastActive:         c.GetLastActive(),
		})
	}
	d.mu.Unlock()

	return json.Marshal(snapshot)
}

// RestoreFromSerialization restores runtime fields after loading from storage.
func (d *Download) RestoreFromSerialization(proto *protocol.Handler, saveStateChan chan<- *Download) error {
	logger.Debugf("Restoring download %s from serialization", d.ID)

	d.mu.Lock()

	protocolHandler, err := proto.GetHandler(d.URL)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	d.protocolHandler = protocolHandler

	chunkManager, err := d.protocolHandler.GetChunkManager(d.ID, d.Config.TempDir)
	if err != nil {
		d.mu.Unlock()
		return err
	}
	d.chunkManager = chunkManager

	d.progressCh = make(chan common.Progress, 10)
	d.saveStateChan = saveStateChan
	d.done = make(chan struct{})
	d.speedCalculator = NewSpeedCalculator(5)

	d.SetStatus(d.Status)
	atomic.StoreInt64(&d.downloaded, d.Downloaded)
	d.SetTotalChunks(int(d.TotalChunks))

	if d.ErrorMessage != "" && d.error == nil {
		d.error = errors.New(d.ErrorMessage)
		logger.Debugf("Restored error message: %s", d.ErrorMessage)
	} else if d.ErrorMessage == "" {
		d.error = nil
	}

	d.Chunks = make([]*chunk.Chunk, 0, d.GetTotalChunks())

	d.mu.Unlock()

	err = d.restoreChunks()
	if err != nil {
		return fmt.Errorf("failed to restore chunks: %w", err)
	}

	d.mu.Lock()
	savingNeeded := false
	currentStatus := d.GetStatus()

	if currentStatus == common.StatusActive {
		logger.Warnf("Download %s was saved in Active state, setting to Paused", d.ID)
		d.SetStatus(common.StatusPaused)
		d.Status = common.StatusPaused
		savingNeeded = true
	}

	if currentStatus == common.StatusCompleted {
		outputPath := filepath.Join(d.Config.Directory, d.Filename)
		if _, statErr := os.Stat(outputPath); os.IsNotExist(statErr) {
			logger.Errorf("Download %s was Completed, but output file %s missing. Setting to Failed.", d.ID, outputPath)
			d.SetStatus(common.StatusFailed)
			d.Status = common.StatusFailed
			d.error = errors.New("output file missing after restore")
			d.ErrorMessage = d.error.Error()
			savingNeeded = true
		} else if statErr != nil {
			logger.Warnf("Error stating output file %s during restore check: %v", outputPath, statErr)
		}
	}
	d.mu.Unlock()

	if savingNeeded {
		select {
		case d.saveStateChan <- d:
			logger.Debugf("Queued download %s for saving after restore adjustments.", d.ID)
		default:
			logger.Warnf("Save channel full, could not queue download %s for saving after restore.", d.ID)
		}
	}

	logger.Debugf("Download %s restored from serialization. Status: %s, Chunks: %d", d.ID, d.GetStatus(), d.GetTotalChunks())
	return nil
}

// restoreChunks restores the chunks from the serialized Download data.
func (d *Download) restoreChunks() error {
	numChunks := len(d.ChunkInfos)
	if numChunks == 0 {
		totalChunksAtomic := d.GetTotalChunks()
		if totalChunksAtomic > 0 {
			logger.Warnf("Download %s: TotalChunks is %d but ChunkInfos is empty during restore.", d.ID, totalChunksAtomic)
			d.SetTotalChunks(0)
		}
		logger.Debugf("No chunk information available to restore for download %s", d.ID)

		return nil
	}

	serializedTotalChunks := d.GetTotalChunks()
	if serializedTotalChunks != numChunks {
		logger.Warnf("Download %s: Serialized TotalChunks (%d) differs from length of ChunkInfos (%d). Using length of ChunkInfos.", d.ID, serializedTotalChunks, numChunks)
		d.SetTotalChunks(numChunks)
	}

	restoredChunks := make([]*chunk.Chunk, numChunks)
	var accumulatedDownloaded int64 = 0

	for i, info := range d.ChunkInfos {
		chunkID, err := uuid.Parse(info.ID)
		if err != nil {
			logger.Errorf("Failed to parse chunk ID %s: %v", info.ID, err)
			return fmt.Errorf("invalid chunk ID %s found during restore", info.ID)
		}

		newChunk := &chunk.Chunk{
			ID:                 chunkID,
			DownloadID:         d.ID,
			StartByte:          info.StartByte,
			EndByte:            info.EndByte,
			TempFilePath:       info.TempFilePath,
			SequentialDownload: info.SequentialDownload,
			LastActive:         info.LastActive,
		}

		newChunk.SetDownloaded(info.Downloaded)
		newChunk.SetStatus(info.Status)
		newChunk.SetRetryCount(info.RetryCount)

		if _, err := os.Stat(newChunk.TempFilePath); os.IsNotExist(err) {
			logger.Debugf("Chunk file %s does not exist, checking directory", newChunk.TempFilePath)
			chunkDir := filepath.Dir(newChunk.TempFilePath)
			if err := os.MkdirAll(chunkDir, 0o755); err != nil && !os.IsExist(err) {
				logger.Warnf("Failed to create chunk directory %s: %v", chunkDir, err)
			}

			if newChunk.GetStatus() == common.StatusCompleted {
				logger.Warnf("Resetting completed chunk %s as file %s is missing", newChunk.ID, newChunk.TempFilePath)
				newChunk.SetStatus(common.StatusPending)
				newChunk.SetDownloaded(0)
			} else {
				if newChunk.GetDownloaded() > 0 {
					logger.Warnf("Resetting downloaded count for chunk %s as file %s is missing", newChunk.ID, newChunk.TempFilePath)
					newChunk.SetDownloaded(0)
				}
			}
		} else if err != nil {
			logger.Warnf("Error stating chunk file %s: %v", newChunk.TempFilePath, err)
		}

		accumulatedDownloaded += newChunk.GetDownloaded()

		restoredChunks[i] = newChunk
		logger.Debugf("Restored chunk %s with status %s, range: %d-%d, downloaded: %d",
			newChunk.ID, newChunk.GetStatus(), newChunk.GetStartByte(), newChunk.GetEndByte(), newChunk.GetDownloaded())
	}

	d.addChunks(restoredChunks...)

	atomic.StoreInt64(&d.downloaded, accumulatedDownloaded)
	logger.Infof("Download %s: Restored %d chunks. Total downloaded recalculated to %d bytes.", d.ID, len(restoredChunks), accumulatedDownloaded)

	d.setProgressFunction()

	return nil
}

// addProgress adds progress to the download atomically.
func (d *Download) addProgress(bytes int64) {
	newDownloaded := atomic.AddInt64(&d.downloaded, bytes)

	if d.speedCalculator != nil {
		d.speedCalculator.AddBytes(bytes)
	}

	select {
	case d.progressCh <- common.Progress{
		DownloadID:     d.ID,
		BytesCompleted: newDownloaded,
		TotalBytes:     d.GetTotalSize(),
		Speed:          d.speedCalculator.GetSpeed(),
		Status:         d.GetStatus(),
		Timestamp:      time.Now(),
	}:
	default:
	}
}

// setProgressFunction sets the progress function for each chunk.
func (d *Download) setProgressFunction() {
	d.mu.RLock()
	defer d.mu.RUnlock()

	logger.Debugf("Setting progress function for %d chunks in download %s", len(d.Chunks), d.ID)

	for i, c := range d.Chunks {
		logger.Debugf("Setting progress function for chunk %d/%d (%s) in download %s",
			i+1, len(d.Chunks), c.ID, d.ID)
		c.SetProgressFunc(d.addProgress)
	}
}
