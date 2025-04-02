package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/errors"
	"github.com/NamanBalaji/tdm/internal/protocol"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

var (
	// ErrDownloadNotFound is returned when a download cannot be found
	ErrDownloadNotFound = errors.New("download not found")

	// ErrInvalidURL is returned for malformed URLs
	ErrInvalidURL = errors.New("invalid URL")

	// ErrDownloadExists is returned when trying to add a duplicate download
	ErrDownloadExists = errors.New("download already exists")

	// ErrEngineNotRunning is returned when an operation requires the engine to be running
	ErrEngineNotRunning = errors.New("engine is not running")
)

type Engine struct {
	mu sync.RWMutex

	downloads       map[uuid.UUID]*downloader.Download
	protocolHandler *protocol.Handler
	connectionPool  *connection.Pool
	chunkManager    *chunk.Manager
	config          *Config
	repository      *repository.BboltRepository
	queueProcessor  *QueueProcessor
	progressMonitor *ProgressMonitor

	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup

	progressCh chan common.Progress

	running bool
}

// runTask runs a function in a goroutine tracked by the WaitGroup
func (e *Engine) runTask(task func()) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		task()
	}()
}

// New creates a new Engine instance
func New(config *Config) (*Engine, error) {
	logger.Infof("Creating new engine instance")

	if config == nil {
		logger.Debugf("No config provided, using default config")
		config = DefaultConfig()
	}

	if err := os.MkdirAll(config.DownloadDir, 0o755); err != nil {
		logger.Errorf("Failed to create download directory %s: %v", config.DownloadDir, err)
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	if err := os.MkdirAll(config.TempDir, 0o755); err != nil {
		logger.Errorf("Failed to create temp directory %s: %v", config.TempDir, err)
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	protocolHandler := protocol.NewHandler()
	connectionPool := connection.NewPool(10, 5*time.Minute)
	chunkManager, err := chunk.NewManager(config.TempDir)
	if err != nil {
		logger.Errorf("Failed to create chunk manager: %v", err)
		return nil, fmt.Errorf("failed to create chunk manager: %w", err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())

	engine := &Engine{
		downloads:       make(map[uuid.UUID]*downloader.Download),
		protocolHandler: protocolHandler,
		connectionPool:  connectionPool,
		chunkManager:    chunkManager,
		config:          config,
		progressCh:      make(chan common.Progress, 1000),
		ctx:             ctx,
		cancelFunc:      cancelFunc,
	}

	logger.Infof("Engine instance created successfully")
	return engine, nil
}

func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		logger.Debugf("Engine already running, skipping initialization")
		return nil
	}

	logger.Infof("Initializing engine")

	if err := e.initRepository(); err != nil {
		logger.Errorf("Failed to initialize repository: %v", err)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	if err := e.loadDownloads(); err != nil {
		logger.Errorf("Failed to load downloads: %v", err)
		return fmt.Errorf("failed to load download: %w", err)
	}

	logger.Debugf("Creating queue processor with max concurrent downloads: %d", e.config.MaxConcurrentDownloads)
	e.queueProcessor = NewQueueProcessor(
		e.config.MaxConcurrentDownloads,
		func(ctx context.Context, download *downloader.Download) error {
			return e.StartDownload(ctx, download.ID)
		},
	)

	e.runTask(func() {
		logger.Debugf("Starting queue processor")
		e.queueProcessor.Start(e.ctx)
	})

	logger.Debugf("Creating progress monitor")
	e.progressMonitor = NewProgressMonitor(e.progressCh)
	e.runTask(func() {
		logger.Debugf("Starting progress monitor")
		e.progressMonitor.Start(e.ctx)
	})

	e.runTask(func() {
		logger.Debugf("Starting periodic save with interval %d seconds", e.config.SaveInterval)
		e.startPeriodicSave(e.ctx)
	})

	if err := e.restoreDownloadStates(); err != nil {
		logger.Errorf("Failed to restore download states: %v", err)
		log.Printf("some download states could not be restored: %v", err)
	}

	queuedCount := 0
	for _, download := range e.downloads {
		if download.Status == common.StatusQueued {
			logger.Debugf("Enqueueing previously queued download: %s", download.ID)
			e.queueProcessor.EnqueueDownload(download, download.Config.Priority)
			queuedCount++
		}
	}
	logger.Debugf("Enqueued %d previously queued downloads", queuedCount)

	e.running = true
	logger.Infof("Engine initialized and running")

	return nil
}

// initRepository initializes the download repository
func (e *Engine) initRepository() error {
	configDir := e.config.ConfigDir
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logger.Errorf("Could not determine home directory: %v", err)
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".tdm")
		logger.Debugf("Using default config directory: %s", configDir)
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		logger.Errorf("Failed to create config directory %s: %v", configDir, err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	dbPath := filepath.Join(configDir, "tdm.db")
	logger.Infof("Initializing repository, dbpath: %v", dbPath)

	repo, err := repository.NewBboltRepository(dbPath)
	if err != nil {
		logger.Errorf("Failed to create repository: %v", err)
		return fmt.Errorf("failed to create repository: %w", err)
	}

	e.repository = repo
	logger.Debugf("Repository initialization complete")

	return nil
}

// loadDownloads loads existing downloads from the repository
func (e *Engine) loadDownloads() error {
	logger.Debugf("Loading downloads from repository")

	downloads, err := e.repository.FindAll()
	if err != nil {
		logger.Errorf("Failed to retrieve downloads from repository: %v", err)
		return fmt.Errorf("failed to retrieve downloads: %w", err)
	}

	for _, download := range downloads {
		logger.Debugf("Restoring download: %s, URL: %s", download.ID, download.URL)
		download.RestoreFromSerialization()

		downloadCtx, cancelFunc := context.WithCancel(e.ctx)
		download.SetContext(downloadCtx, cancelFunc)

		if err := e.restoreChunks(download); err != nil {
			logger.Warnf("Failed to restore chunks for download %s: %v", download.ID, err)
		}

		e.downloads[download.ID] = download
		logger.Debugf("Download %s restored with status: %s", download.ID, download.Status)
	}

	logger.Infof("Loaded %d download(s) from repository", len(e.downloads))
	return nil
}

// restoreChunks recreates chunk objects from serialized chunk info
func (e *Engine) restoreChunks(download *downloader.Download) error {
	if len(download.ChunkInfos) == 0 {
		logger.Debugf("No chunks to restore for download %s", download.ID)
		return nil
	}

	logger.Debugf("Restoring %d chunks for download %s", len(download.ChunkInfos), download.ID)
	chunks := make([]*chunk.Chunk, len(download.ChunkInfos))

	for i, info := range download.ChunkInfos {
		chunkID, err := uuid.Parse(info.ID)
		if err != nil {
			logger.Errorf("Failed to parse chunk ID %s: %v", info.ID, err)
			return err
		}

		newChunk := &chunk.Chunk{
			ID:                 chunkID,
			DownloadID:         download.ID,
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

			if newChunk.Status == common.StatusCompleted {
				logger.Debugf("Resetting completed chunk %s as file is missing", newChunk.ID)
				newChunk.Status = common.StatusPending
				newChunk.Downloaded = 0
			}
		}

		chunks[i] = newChunk
		logger.Debugf("Restored chunk %s with status %s, range: %d-%d, downloaded: %d",
			newChunk.ID, newChunk.Status, newChunk.StartByte, newChunk.EndByte, newChunk.Downloaded)
	}

	download.Chunks = chunks
	download.SetProgressFunction()
	logger.Debugf("All chunks restored for download %s", download.ID)

	return nil
}

// AddDownload adds a new download to the engine
func (e *Engine) AddDownload(url string, config *downloader.Config) (uuid.UUID, error) {
	logger.Infof("Adding download for URL: %s", url)

	if url == "" {
		logger.Errorf("Cannot add download with empty URL")
		return uuid.Nil, ErrInvalidURL
	}

	if !e.running {
		logger.Errorf("Cannot add download, engine is not running")
		return uuid.Nil, ErrEngineNotRunning
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicate URL
	for _, download := range e.downloads {
		if download.URL == url &&
			download.Status != common.StatusCompleted &&
			download.Status != common.StatusFailed &&
			download.Status != common.StatusCancelled {
			logger.Warnf("Download already exists for URL: %s", url)
			return uuid.Nil, ErrDownloadExists
		}
	}

	dConfig := config
	if dConfig == nil {
		logger.Debugf("No config provided, using default download config")
		dConfig = &downloader.Config{}
	}

	if dConfig.Directory == "" {
		dConfig.Directory = e.config.DownloadDir
		logger.Debugf("Using default download directory: %s", dConfig.Directory)
	}
	if dConfig.Connections <= 0 {
		dConfig.Connections = e.config.MaxConnectionsPerDownload
		logger.Debugf("Using default connections: %d", dConfig.Connections)
	}
	if dConfig.MaxRetries <= 0 {
		dConfig.MaxRetries = e.config.MaxRetries
		logger.Debugf("Using default max retries: %d", dConfig.MaxRetries)
	}
	if dConfig.RetryDelay <= 0 {
		dConfig.RetryDelay = time.Duration(e.config.RetryDelay) * time.Second
		logger.Debugf("Using default retry delay: %v", dConfig.RetryDelay)
	}

	logger.Debugf("Initializing download for URL: %s", url)
	info, err := e.protocolHandler.Initialize(e.ctx, url, dConfig)
	if err != nil {
		logger.Errorf("Failed to initialize download for URL %s: %v", url, err)
		return uuid.Nil, fmt.Errorf("failed to initialize download: %w", err)
	}

	logger.Debugf("Download initialized, filename: %s, size: %d, supports ranges: %v",
		info.Filename, info.TotalSize, info.SupportsRanges)

	if err := os.MkdirAll(dConfig.Directory, 0o755); err != nil {
		logger.Errorf("Failed to create directory %s: %v", dConfig.Directory, err)
		return uuid.Nil, fmt.Errorf("failed to create directory: %w", err)
	}

	download := downloader.NewDownload(url, info.Filename, dConfig)
	download.TotalSize = info.TotalSize

	logger.Debugf("Created download with ID: %s", download.ID)

	downloadCtx, cancelFunc := context.WithCancel(e.ctx)
	download.SetContext(downloadCtx, cancelFunc)

	logger.Debugf("Creating chunks for download %s", download.ID)
	chunks, err := e.chunkManager.CreateChunks(download.ID, info.TotalSize, info.SupportsRanges, dConfig.Connections, download.AddProgress)
	if err != nil {
		logger.Errorf("Failed to create chunks: %v", err)
		return uuid.Nil, fmt.Errorf("failed to create chunks: %w", err)
	}
	download.Chunks = chunks
	logger.Debugf("Created %d chunks for download %s", len(chunks), download.ID)

	logger.Debugf("Saving download %s", download.ID)
	if err := e.saveDownload(download); err != nil {
		logger.Errorf("Failed to save download %s: %v", download.ID, err)
		return uuid.Nil, fmt.Errorf("failed to save download: %w", err)
	}

	e.downloads[download.ID] = download

	if e.config.AutoStartDownloads {
		logger.Debugf("Auto-starting download %s", download.ID)
		e.queueProcessor.EnqueueDownload(download, download.Config.Priority)
	} else {
		logger.Debugf("Setting download %s as pending (auto-start disabled)", download.ID)
		download.Status = common.StatusPending
		if err := e.saveDownload(download); err != nil {
			logger.Warnf("Failed to save download %s: %v", download.ID, err)
		}
	}

	logger.Infof("Download added successfully with ID: %s", download.ID)
	return download.ID, nil
}

// GetDownload retrieves a download by ID string
func (e *Engine) GetDownload(id uuid.UUID) (*downloader.Download, error) {
	logger.Debugf("Getting download with ID: %s", id)

	e.mu.RLock()
	defer e.mu.RUnlock()

	download, ok := e.downloads[id]
	if !ok {
		logger.Debugf("Download not found with ID: %s", id)
		return nil, ErrDownloadNotFound
	}

	return download, nil
}

// ListDownloads returns all downloads
func (e *Engine) ListDownloads() []*downloader.Download {
	logger.Debugf("Listing all downloads")

	e.mu.RLock()
	defer e.mu.RUnlock()

	downloads := make([]*downloader.Download, 0, len(e.downloads))
	for _, download := range e.downloads {
		downloads = append(downloads, download)
	}

	logger.Debugf("Returning %d downloads", len(downloads))
	return downloads
}

// RemoveDownload removes a download from the manager
func (e *Engine) RemoveDownload(id uuid.UUID, removeFiles bool) error {
	logger.Infof("Removing download %s (removeFiles: %v)", id, removeFiles)

	if !e.running {
		logger.Errorf("Cannot remove download, engine is not running")
		return ErrEngineNotRunning
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	download, ok := e.downloads[id]
	if !ok {
		logger.Warnf("Download not found for removal: %s", id)
		return ErrDownloadNotFound
	}

	// Cancel download if active
	if download.Status == common.StatusActive {
		logger.Debugf("Cancelling active download %s before removal", id)
		err := e.CancelDownload(id, removeFiles)
		if err != nil {
			logger.Errorf("Failed to cancel download %s: %v", id, err)
			return fmt.Errorf("failed to cancel download: %w", err)
		}
	}

	if removeFiles {
		logger.Debugf("Cleaning up chunk files for download %s", id)
		if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
			logger.Warnf("Failed to clean up chunks: %v", err)
		}

		outputPath := filepath.Join(download.Config.Directory, download.Filename)
		logger.Debugf("Removing output file: %s", outputPath)
		if _, err := os.Stat(outputPath); err == nil {
			if err := os.Remove(outputPath); err != nil {
				logger.Warnf("Failed to remove output file: %v", err)
			}
		}
	}

	if e.repository != nil {
		logger.Debugf("Deleting download %s from repository", id)
		if err := e.repository.Delete(id); err != nil && !errors.Is(err, repository.ErrDownloadNotFound) {
			logger.Errorf("Failed to delete download from repository: %v", err)
			return fmt.Errorf("failed to delete download from repository: %w", err)
		}
	}

	delete(e.downloads, id)
	logger.Infof("Download %s removed successfully", id)

	return nil
}

// GetGlobalStats returns global download statistics
func (e *Engine) GetGlobalStats() common.GlobalStats {
	logger.Debugf("Getting global download stats")

	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := common.GlobalStats{
		MaxConcurrent: e.config.MaxConcurrentDownloads,
	}

	for _, download := range e.downloads {
		switch download.Status {
		case common.StatusActive:
			stats.ActiveDownloads++
			stats.CurrentConcurrent++
		case common.StatusQueued:
			stats.QueuedDownloads++
		case common.StatusCompleted:
			stats.CompletedDownloads++
		case common.StatusFailed, common.StatusCancelled:
			stats.FailedDownloads++
		case common.StatusPaused:
			stats.PausedDownloads++
		}

		stats.TotalDownloaded += download.Downloaded

		if download.Status == common.StatusActive {
			dStats := download.GetStats()
			stats.CurrentSpeed += dStats.Speed
		}
	}

	if stats.ActiveDownloads > 0 {
		stats.AverageSpeed = stats.CurrentSpeed / int64(stats.ActiveDownloads)
	}

	logger.Debugf("Stats: active=%d, queued=%d, completed=%d, failed=%d, paused=%d, speed=%d B/s",
		stats.ActiveDownloads, stats.QueuedDownloads, stats.CompletedDownloads,
		stats.FailedDownloads, stats.PausedDownloads, stats.CurrentSpeed)

	return stats
}

// PauseDownload pauses an active download
func (e *Engine) PauseDownload(id uuid.UUID) error {
	logger.Infof("Pausing download: %s", id)

	if !e.running {
		logger.Errorf("Cannot pause download, engine is not running")
		return ErrEngineNotRunning
	}

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return fmt.Errorf("failed to get download: %w", err)
	}

	// Can only pause active downloads
	if download.Status != common.StatusActive {
		logger.Debugf("Download %s is not active (status: %s), skipping pause", id, download.Status)
		return nil
	}

	logger.Debugf("Cancelling context for download %s", id)
	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusPaused
	logger.Debugf("Download %s status set to paused", id)

	if err := e.saveDownload(download); err != nil {
		logger.Errorf("Failed to save download %s after pausing: %v", id, err)
		return fmt.Errorf("failed to save download: %w", err)
	}

	logger.Infof("Download %s paused successfully", id)
	return nil
}

// ResumeDownload resumes a paused download
func (e *Engine) ResumeDownload(ctx context.Context, id uuid.UUID) error {
	logger.Infof("Resuming download: %s", id)

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return fmt.Errorf("failed to get download: %w", err)
	}

	if download.Status != common.StatusPaused && download.Status != common.StatusFailed {
		logger.Debugf("Download %s is not paused or failed (status: %s), skipping resume",
			id, download.Status)
		return nil
	}

	logger.Debugf("Enqueueing download %s for resumption", id)
	e.queueProcessor.EnqueueDownload(download, download.Config.Priority)
	logger.Infof("Download %s resumed successfully", id)

	return nil
}

// CancelDownload cancels a download
func (e *Engine) CancelDownload(id uuid.UUID, removeFiles bool) error {
	logger.Infof("Cancelling download %s (removeFiles: %v)", id, removeFiles)

	if !e.running {
		logger.Errorf("Cannot cancel download, engine is not running")
		return ErrEngineNotRunning
	}

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return fmt.Errorf("failed to get download: %w", err)
	}

	if download.Status == common.StatusCompleted {
		logger.Debugf("Download %s is already completed, skipping cancellation", id)
		return nil
	}

	logger.Debugf("Cancelling context for download %s", id)
	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusCancelled
	logger.Debugf("Download %s status set to cancelled", id)

	if removeFiles {
		logger.Debugf("Cleaning up chunk files for download %s", id)
		if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
			logger.Warnf("Failed to clean up chunks: %v", err)
		}
	}

	if err := e.saveDownload(download); err != nil {
		logger.Errorf("Failed to save download %s after cancellation: %v", id, err)
		return fmt.Errorf("failed to save download: %w", err)
	}

	logger.Infof("Download %s cancelled successfully", id)
	return nil
}

// StartDownload initiates a download
func (e *Engine) StartDownload(ctx context.Context, id uuid.UUID) error {
	logger.Infof("Starting download: %s", id)

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return err
	}

	stats := download.GetStats()
	if stats.Status == common.StatusActive {
		logger.Debugf("Download %s is already active, skipping start", id)
		return nil
	}

	logger.Debugf("Creating new context for download %s", id)
	downloadCtx, cancelFunc := context.WithCancel(ctx)
	download.SetContext(downloadCtx, cancelFunc)

	download.Status = common.StatusActive
	download.StartTime = time.Now()
	logger.Debugf("Download %s status set to active", id)

	if err := e.saveDownload(download); err != nil {
		logger.Errorf("Failed to save download %s after starting: %v", id, err)
		return fmt.Errorf("failed to save download: %w", err)
	}

	e.runTask(func() {
		logger.Debugf("Processing download %s in background", id)
		e.processDownload(download)
	})

	logger.Infof("Download %s started successfully", id)
	return nil
}

// processDownload handles the actual download process
func (e *Engine) processDownload(download *downloader.Download) {
	logger.Infof("Processing download %s: %s", download.ID, download.URL)

	chunks := e.getPendingChunks(download)
	logger.Debugf("Found %d pending chunks for download %s", len(chunks), download.ID)

	defer e.queueProcessor.NotifyDownloadCompletion(download.ID)

	if len(chunks) == 0 {
		logger.Debugf("No pending chunks for download %s, finishing", download.ID)
		if err := e.finishDownload(download); err != nil {
			logger.Errorf("Errorf finishing download %s: %s", download.ID, err)
		}
		return
	}

	logger.Debugf("Creating error group for download %s with %d chunks", download.ID, len(chunks))
	g, ctx := errgroup.WithContext(download.Context())

	sem := make(chan struct{}, download.Config.Connections)
	logger.Debugf("Using semaphore with %d slots for download %s", download.Config.Connections, download.ID)

	for i, chunk := range chunks {
		chunkCopy := chunk
		chunkIdx := i

		g.Go(func() error {
			logger.Debugf("Starting goroutine for chunk %d (%s) of download %s",
				chunkIdx, chunkCopy.ID, download.ID)

			select {
			case sem <- struct{}{}:
				logger.Debugf("Acquired semaphore slot for chunk %s", chunkCopy.ID)
				defer func() {
					<-sem
					logger.Debugf("Released semaphore slot for chunk %s", chunkCopy.ID)
				}()
			case <-ctx.Done():
				logger.Debugf("Context cancelled while waiting for semaphore for chunk %s", chunkCopy.ID)
				return errors.NewContextError(ctx.Err(), download.URL)
			}

			logger.Debugf("Downloading chunk %s (range: %d-%d) for download %s",
				chunkCopy.ID, chunkCopy.StartByte, chunkCopy.EndByte, download.ID)
			return e.downloadChunkWithRetries(ctx, download, chunkCopy)
		})
	}

	logger.Debugf("Waiting for all chunks to complete for download %s", download.ID)
	err := g.Wait()

	if err != nil {
		var downloadErr *errors.DownloadError
		if errors.As(err, &downloadErr) && downloadErr.Category == errors.CategoryContext {
			logger.Infof("Download %s paused due to context cancellation", download.ID)
			download.Status = common.StatusPaused
			if saveErr := e.saveDownload(download); saveErr != nil {
				logger.Errorf("Failed to save download %s after pausing: %s", download.ID, saveErr)
			}
		} else {
			logger.Errorf("Download %s failed: %v", download.ID, err)
			e.handleDownloadFailure(download, err)
		}
	} else {
		logger.Infof("All chunks completed successfully for download %s", download.ID)
		if err := e.finishDownload(download); err != nil {
			logger.Errorf("Errorf finishing download %s: %s", download.ID, err)
		}
	}
}

// downloadChunkWithRetries downloads a chunk with intelligent retry logic
func (e *Engine) downloadChunkWithRetries(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	logger.Debugf("Downloading chunk %s with retries (max: %d)", chunk.ID, download.Config.MaxRetries)

	err := e.downloadChunk(ctx, download, chunk)

	if err == nil || errors.Is(err, context.Canceled) {
		if err == nil {
			logger.Debugf("Chunk %s downloaded successfully", chunk.ID)
		} else {
			logger.Debugf("Chunk %s download cancelled", chunk.ID)
		}
		return err
	}

	// Log the error with retry ability information
	logger.Errorf("Download error for chunk %s: %v (retryable: %v)",
		chunk.ID, err, errors.IsRetryable(err))

	// Retry logic
	for chunk.RetryCount < download.Config.MaxRetries {
		// Reset the chunk for retry
		chunk.Reset()
		logger.Debugf("Reset chunk %s for retry attempt %d/%d",
			chunk.ID, chunk.RetryCount, download.Config.MaxRetries)

		backoff := calculateBackoff(chunk.RetryCount, download.Config.RetryDelay)
		logger.Debugf("Waiting %v before retrying chunk %s", backoff, chunk.ID)

		select {
		case <-time.After(backoff):
			logger.Infof("Retrying chunk %s (attempt %d/%d)",
				chunk.ID, chunk.RetryCount+1, download.Config.MaxRetries)

			err = e.downloadChunk(ctx, download, chunk)
			if err == nil || errors.Is(err, context.Canceled) {
				if err == nil {
					logger.Debugf("Chunk %s retry succeeded", chunk.ID)
				} else {
					logger.Debugf("Chunk %s retry cancelled", chunk.ID)
				}
				return err
			}

			logger.Errorf("Download retry failed for chunk %s: %v (retryable: %v)",
				chunk.ID, err, errors.IsRetryable(err))

			if !errors.IsRetryable(err) {
				logger.Errorf("Errorf not retryable for chunk %s, giving up", chunk.ID)
				return err
			}

		case <-ctx.Done():
			logger.Debugf("Context cancelled while waiting to retry chunk %s", chunk.ID)
			chunk.Status = common.StatusPaused
			return errors.NewContextError(ctx.Err(), fmt.Sprintf("chunk %s", chunk.ID))
		}
	}

	logger.Errorf("Chunk %s failed after %d retry attempts", chunk.ID, download.Config.MaxRetries)
	return fmt.Errorf("chunk %s failed after %d attempts: %w", chunk.ID, download.Config.MaxRetries, err)
}

// downloadChunk downloads a single chunk
func (e *Engine) downloadChunk(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	logger.Debugf("Downloading chunk %s (range: %d-%d) for download %s",
		chunk.ID, chunk.StartByte, chunk.EndByte, download.ID)

	chunk.Status = common.StatusActive

	handler, err := e.protocolHandler.GetHandler(download.URL)
	if err != nil {
		logger.Errorf("Failed to get protocol handler for URL %s: %v", download.URL, err)
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return errors.NewNetworkError(err, download.URL, false)
	}

	logger.Debugf("Creating connection for chunk %s", chunk.ID)
	conn, err := handler.CreateConnection(ctx, download.URL, chunk, download.Config)
	if err != nil {
		logger.Errorf("Failed to create connection for chunk %s: %v", chunk.ID, err)
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return err // Already classified by protocol handler
	}

	logger.Debugf("Registering connection for chunk %s with connection pool", chunk.ID)
	e.connectionPool.RegisterConnection(conn)
	defer e.connectionPool.ReleaseConnection(conn)

	chunk.Connection = conn
	logger.Debugf("Starting download for chunk %s", chunk.ID)
	err = chunk.Download(ctx)

	if err != nil {
		// If we received a context cancellation, wrap it with our error system
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debugf("Download of chunk %s cancelled due to context", chunk.ID)
			return errors.NewContextError(err, download.URL)
		}
		// Other errors are already properly categorized
		logger.Errorf("Errorf downloading chunk %s: %v", chunk.ID, err)
		return err
	}

	logger.Debugf("Chunk %s downloaded successfully", chunk.ID)
	return nil
}

// getPendingChunks returns chunks that need downloading
func (e *Engine) getPendingChunks(download *downloader.Download) []*chunk.Chunk {
	var pending []*chunk.Chunk
	for _, chunk := range download.Chunks {
		if chunk.Status != common.StatusCompleted {
			pending = append(pending, chunk)
		}
	}
	logger.Debugf("Found %d pending chunks for download %s", len(pending), download.ID)
	return pending
}

// handleDownloadFailure updates download state on failure
func (e *Engine) handleDownloadFailure(download *downloader.Download, err error) {
	logger.Errorf("Handling download failure for %s: %v", download.ID, err)

	download.Status = common.StatusFailed
	download.Error = err
	download.ErrorMessage = err.Error()

	if saveErr := e.saveDownload(download); saveErr != nil {
		logger.Errorf("Failed to save download %s after failure: %s", download.ID, saveErr)
	}

	logger.Debugf("Download %s marked as failed", download.ID)
}

func (e *Engine) finishDownload(download *downloader.Download) error {
	logger.Infof("Finishing download %s", download.ID)

	for _, chunk := range download.Chunks {
		if chunk.Status != common.StatusCompleted {
			err := fmt.Errorf("cannot finish download: chunk %s is in state %s", chunk.ID, chunk.Status)
			logger.Errorf("Cannot finish download %s: %v", download.ID, err)
			e.handleDownloadFailure(download, err)
			return err
		}
	}

	targetPath := filepath.Join(download.Config.Directory, download.Filename)
	logger.Debugf("Merging chunks for download %s to %s", download.ID, targetPath)

	if err := e.chunkManager.MergeChunks(download.Chunks, targetPath); err != nil {
		logger.Errorf("Failed to merge chunks for download %s: %v", download.ID, err)
		e.handleDownloadFailure(download, err)
		return fmt.Errorf("failed to merge chunks: %w", err)
	}

	download.Status = common.StatusCompleted
	download.EndTime = time.Now()
	logger.Debugf("Download %s marked as completed", download.ID)

	logger.Debugf("Cleaning up chunks for download %s", download.ID)
	if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
		logger.Warnf("Failed to cleanup chunks for download %s: %s", download.ID, err)
	}

	logger.Debugf("Saving download %s", download.ID)
	if err := e.saveDownload(download); err != nil {
		logger.Warnf("Failed to save download %s: %s", download.ID, err)
	}

	logger.Infof("Download %s finished successfully", download.ID)
	return nil
}

// restoreDownloadStates restores the state of downloads based on their status
func (e *Engine) restoreDownloadStates() error {
	logger.Infof("Restoring download states")
	var lastErr error

	for _, download := range e.downloads {
		logger.Debugf("Restoring state for download %s with status %s", download.ID, download.Status)

		switch download.Status {
		case common.StatusActive:
			// Downloads that were active should be paused on restart
			logger.Debugf("Changing active download %s to paused", download.ID)
			download.Status = common.StatusPaused
			if err := e.saveDownload(download); err != nil {
				lastErr = err
				logger.Errorf("Errorf setting download %s to paused: %v", download.ID, err)
			}

		case common.StatusQueued:
			logger.Debugf("Download %s remains queued", download.ID)
			download.Status = common.StatusQueued

		case common.StatusFailed, common.StatusCancelled:
			logger.Debugf("Download %s remains in state %s", download.ID, download.Status)
			continue

		case common.StatusPaused:
			logger.Debugf("Download %s remains paused", download.ID)
			continue

		case common.StatusCompleted:
			outputPath := filepath.Join(download.Config.Directory, download.Filename)
			logger.Debugf("Checking if completed download %s file exists at %s", download.ID, outputPath)
			if _, err := os.Stat(outputPath); os.IsNotExist(err) {
				logger.Warnf("Output file missing for completed download %s, marking as failed", download.ID)
				download.Status = common.StatusFailed
				download.Error = fmt.Errorf("output file missing")
				if err := e.saveDownload(download); err != nil {
					lastErr = err
					logger.Errorf("Errorf updating download %s status: %v", download.ID, err)
				}
			}
		}
	}

	logger.Infof("Download states restored")
	return lastErr
}

// Shutdown gracefully stops the engine, saving all download states
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	if !e.running {
		logger.Debugf("Engine not running, skipping shutdown")
		e.mu.Unlock()
		return nil
	}

	logger.Infof("Starting engine shutdown...")

	// Mark as not running
	e.running = false

	// Get all active downloads while holding the lock
	activeDownloadIDs := make([]uuid.UUID, 0)
	for id, download := range e.downloads {
		if download.Status == common.StatusActive {
			activeDownloadIDs = append(activeDownloadIDs, id)
		}
	}
	e.mu.Unlock()

	// Pause all active downloads using the existing PauseDownload function
	logger.Infof("Pausing %d active downloads...", len(activeDownloadIDs))
	for _, id := range activeDownloadIDs {
		if err := e.PauseDownload(id); err != nil {
			logger.Errorf("Errorf pausing download %s: %v", id, err)
		}
	}

	// Cancel the context to signal all operations to stop
	logger.Debugf("Cancelling engine context")
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	// First stop services that might be starting new tasks
	logger.Infof("Stopping queue processor...")
	if e.queueProcessor != nil {
		e.queueProcessor.Stop()
	}

	logger.Infof("Stopping progress monitor...")
	if e.progressMonitor != nil {
		e.progressMonitor.Stop()
	}

	// Save all downloads
	logger.Infof("Saving download states...")
	e.saveAllDownloads()

	// Create a timeout context for the remaining shutdown operations
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Wait for tasks to complete with a timeout
	logger.Infof("Waiting for tasks to complete...")
	waitChan := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		logger.Infof("All tasks completed gracefully")
	case <-shutdownCtx.Done():
		logger.Warnf("Shutdown timed out, some tasks may not have completed")
	}

	// Now it's safe to close resources
	logger.Infof("Closing connection pool...")
	if e.connectionPool != nil {
		e.connectionPool.CloseAll()
	}

	if e.repository != nil {
		logger.Infof("Closing repository...")
		if err := e.repository.Close(); err != nil {
			logger.Errorf("Errorf closing repository: %v", err)
		}
	}

	// Close the progress channel last
	logger.Infof("Closing progress channel...")
	close(e.progressCh)

	logger.Infof("Engine shutdown complete")
	return nil
}

// startPeriodicSave starts a ticker to save download states periodically
func (e *Engine) startPeriodicSave(ctx context.Context) {
	interval := time.Duration(e.config.SaveInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	logger.Debugf("Starting periodic save with interval %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Debugf("Performing periodic save of all downloads")
			e.saveAllDownloads()
		case <-ctx.Done():
			logger.Debugf("Periodic save stopped due to context cancellation")
			return
		}
	}
}

// saveAllDownloads saves the state of all downloads
func (e *Engine) saveAllDownloads() {
	if e.repository == nil {
		logger.Warnf("Repository not initialized, skipping save")
		return
	}

	e.mu.RLock()
	downloads := make([]*downloader.Download, 0, len(e.downloads))
	for _, dl := range e.downloads {
		downloads = append(downloads, dl)
	}
	e.mu.RUnlock()

	logger.Debugf("Saving %d downloads", len(downloads))
	saveCount := 0
	for _, download := range downloads {
		if err := e.saveDownload(download); err != nil {
			logger.Errorf("Errorf saving download %s: %v", download.ID, err)
		} else {
			saveCount++
		}
	}
	logger.Debugf("Successfully saved %d downloads", saveCount)
}

// saveDownload persists a download to the repository
func (e *Engine) saveDownload(download *downloader.Download) error {
	if e.repository == nil {
		logger.Errorf("Cannot save download %s: repository not initialized", download.ID)
		return fmt.Errorf("repository not initialized")
	}

	logger.Debugf("Preparing download %s for serialization", download.ID)
	download.PrepareForSerialization()

	logger.Debugf("Saving download %s to repository", download.ID)
	return e.repository.Save(download)
}
