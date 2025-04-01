package engine

import (
	"context"
	"fmt"
	"github.com/NamanBalaji/tdm/internal/logger"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	if config == nil {
		config = DefaultConfig()
	}

	if err := os.MkdirAll(config.DownloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	if err := os.MkdirAll(config.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	protocolHandler := protocol.NewHandler()
	connectionPool := connection.NewPool(10, 5*time.Minute)
	chunkManager, err := chunk.NewManager(config.TempDir)
	if err != nil {
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

	return engine, nil
}

func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return nil
	}

	logger.Info("Initializing engine")

	if err := e.initRepository(); err != nil {
		logger.Error("Failed to initialize repository: %v", err)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	if err := e.loadDownloads(); err != nil {
		logger.Error("Failed to load downloads: %v", err)
		return fmt.Errorf("failed to load download: %w", err)
	}

	e.queueProcessor = NewQueueProcessor(
		e.config.MaxConcurrentDownloads,
		func(ctx context.Context, download *downloader.Download) error {
			return e.StartDownload(ctx, download.ID)
		},
	)

	e.runTask(func() {
		e.queueProcessor.Start(e.ctx)
	})

	e.progressMonitor = NewProgressMonitor(e.progressCh)
	e.runTask(func() {
		e.progressMonitor.Start(e.ctx)
	})

	e.runTask(func() {
		e.startPeriodicSave(e.ctx)
	})

	if err := e.restoreDownloadStates(); err != nil {
		logger.Error("Failed to restore download states: %v", err)
		log.Printf("some download states could not be restored: %v", err)
	}

	for _, download := range e.downloads {
		if download.Status == common.StatusQueued {
			e.queueProcessor.EnqueueDownload(download, download.Config.Priority)
		}
	}

	e.running = true
	logger.Info("Engine initialized and running")

	return nil
}

// initRepository initializes the download repository
func (e *Engine) initRepository() error {
	configDir := e.config.ConfigDir
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".tdm")
	}

	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	dbPath := filepath.Join(configDir, "tdm.db")
	repo, err := repository.NewBboltRepository(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	e.repository = repo
	logger.Info("Initializing repository, dbpath: %v", dbPath)

	return nil
}

// loadDownloads loads existing downloads from the repository
func (e *Engine) loadDownloads() error {
	downloads, err := e.repository.FindAll()
	if err != nil {
		return fmt.Errorf("failed to retrieve downloads: %w", err)
	}

	for _, download := range downloads {
		download.RestoreFromSerialization()

		downloadCtx, cancelFunc := context.WithCancel(e.ctx)
		download.SetContext(downloadCtx, cancelFunc)

		if err := e.restoreChunks(download); err != nil {
			logger.Warn("Failed to restore chunks for download %s: %v", download.ID, err)
		}

		e.downloads[download.ID] = download
	}

	logger.Info("Loaded %d download(s) from repository", len(e.downloads))
	return nil
}

// restoreChunks recreates chunk objects from serialized chunk info
func (e *Engine) restoreChunks(download *downloader.Download) error {
	if len(download.ChunkInfos) == 0 {
		return nil
	}

	chunks := make([]*chunk.Chunk, len(download.ChunkInfos))

	for i, info := range download.ChunkInfos {
		chunkID, err := uuid.Parse(info.ID)
		if err != nil {
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
			chunkDir := filepath.Dir(newChunk.TempFilePath)
			if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
				if err := os.MkdirAll(chunkDir, 0o755); err != nil {
					logger.Warn("Failed to create chunk directory %s: %v", chunkDir, err)
				}
			}

			if newChunk.Status == common.StatusCompleted {
				newChunk.Status = common.StatusPending
				newChunk.Downloaded = 0
			}
		}

		chunks[i] = newChunk
	}

	download.Chunks = chunks

	download.SetProgressFunction()

	return nil
}

// AddDownload adds a new download to the engine
func (e *Engine) AddDownload(url string, config *downloader.Config) (uuid.UUID, error) {
	if url == "" {
		return uuid.Nil, ErrInvalidURL
	}

	if !e.running {
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
			return uuid.Nil, ErrDownloadExists
		}
	}

	dConfig := config
	if dConfig == nil {
		dConfig = &downloader.Config{}
	}

	if dConfig.Directory == "" {
		dConfig.Directory = e.config.DownloadDir
	}
	if dConfig.Connections <= 0 {
		dConfig.Connections = e.config.MaxConnectionsPerDownload
	}
	if dConfig.MaxRetries <= 0 {
		dConfig.MaxRetries = e.config.MaxRetries
	}
	if dConfig.RetryDelay <= 0 {
		dConfig.RetryDelay = time.Duration(e.config.RetryDelay) * time.Second
	}

	info, err := e.protocolHandler.Initialize(e.ctx, url, dConfig)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to initialize download: %w", err)
	}

	if err := os.MkdirAll(dConfig.Directory, 0o755); err != nil {
		return uuid.Nil, fmt.Errorf("failed to create directory: %w", err)
	}

	download := downloader.NewDownload(url, info.Filename, dConfig)
	download.TotalSize = info.TotalSize

	downloadCtx, cancelFunc := context.WithCancel(e.ctx)
	download.SetContext(downloadCtx, cancelFunc)

	chunks, err := e.chunkManager.CreateChunks(download.ID, info.TotalSize, info.SupportsRanges, dConfig.Connections, download.AddProgress)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create chunks: %w", err)
	}
	download.Chunks = chunks

	if err := e.saveDownload(download); err != nil {
		return uuid.Nil, fmt.Errorf("failed to save download: %w", err)
	}

	e.downloads[download.ID] = download
	if e.config.AutoStartDownloads {
		e.queueProcessor.EnqueueDownload(download, download.Config.Priority)
	} else {
		download.Status = common.StatusPending
		if err := e.saveDownload(download); err != nil {
			log.Printf("Warning: Failed to save download %s: %v", download.ID, err)
		}
	}

	return download.ID, nil
}

// GetDownload retrieves a download by ID string
func (e *Engine) GetDownload(id uuid.UUID) (*downloader.Download, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	download, ok := e.downloads[id]
	if !ok {
		return nil, ErrDownloadNotFound
	}

	return download, nil
}

// ListDownloads returns all downloads
func (e *Engine) ListDownloads() []*downloader.Download {
	e.mu.RLock()
	defer e.mu.RUnlock()

	downloads := make([]*downloader.Download, 0, len(e.downloads))
	for _, download := range e.downloads {
		downloads = append(downloads, download)
	}

	return downloads
}

// RemoveDownload removes a download from the manager
func (e *Engine) RemoveDownload(id uuid.UUID, removeFiles bool) error {
	if !e.running {
		return ErrEngineNotRunning
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	download, ok := e.downloads[id]
	if !ok {
		return ErrDownloadNotFound
	}

	// Cancel download if active
	if download.Status == common.StatusActive {
		err := e.CancelDownload(id, removeFiles)
		if err != nil {
			return fmt.Errorf("failed to cancel download: %w", err)
		}
	}

	if removeFiles {
		if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
			log.Printf("Warning: Failed to clean up chunks: %v", err)
		}

		outputPath := filepath.Join(download.Config.Directory, download.Filename)
		if _, err := os.Stat(outputPath); err == nil {
			if err := os.Remove(outputPath); err != nil {
				log.Printf("Warning: Failed to remove output file: %v", err)
			}
		}
	}

	if e.repository != nil {
		if err := e.repository.Delete(id); err != nil && !errors.Is(err, repository.ErrDownloadNotFound) {
			return fmt.Errorf("failed to delete download from repository: %w", err)
		}
	}

	delete(e.downloads, id)

	return nil
}

// GetGlobalStats returns global download statistics
func (e *Engine) GetGlobalStats() common.GlobalStats {
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

	return stats
}

// PauseDownload pauses an active download
func (e *Engine) PauseDownload(id uuid.UUID) error {
	if !e.running {
		return ErrEngineNotRunning
	}

	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}

	// Can only pause active downloads
	if download.Status != common.StatusActive {
		return nil
	}

	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusPaused

	if err := e.saveDownload(download); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	return nil
}

// ResumeDownload resumes a paused download
func (e *Engine) ResumeDownload(ctx context.Context, id uuid.UUID) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}

	if download.Status != common.StatusPaused && download.Status != common.StatusFailed {
		return nil
	}

	e.queueProcessor.EnqueueDownload(download, download.Config.Priority)

	return nil
}

// CancelDownload cancels a download
func (e *Engine) CancelDownload(id uuid.UUID, removeFiles bool) error {
	if !e.running {
		return ErrEngineNotRunning
	}

	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}

	if download.Status == common.StatusCompleted {
		return nil
	}

	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusCancelled
	if removeFiles {
		if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
			log.Printf("Warning: Failed to clean up chunks: %v", err)
		}
	}

	if err := e.saveDownload(download); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	return nil
}

// StartDownload initiates a download
func (e *Engine) StartDownload(ctx context.Context, id uuid.UUID) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	stats := download.GetStats()
	if stats.Status == common.StatusActive {
		return nil
	}

	downloadCtx, cancelFunc := context.WithCancel(ctx)
	download.SetContext(downloadCtx, cancelFunc)

	download.Status = common.StatusActive
	download.StartTime = time.Now()

	if err := e.saveDownload(download); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	e.runTask(func() {
		e.processDownload(download)
	})

	return nil
}

// processDownload handles the actual download process
func (e *Engine) processDownload(download *downloader.Download) {
	chunks := e.getPendingChunks(download)
	defer e.queueProcessor.NotifyDownloadCompletion(download.ID)

	if len(chunks) == 0 {
		if err := e.finishDownload(download); err != nil {
			log.Printf("error finishing download: %s", err)
		}
		return
	}

	g, ctx := errgroup.WithContext(download.Context())

	sem := make(chan struct{}, download.Config.Connections)

	for _, chunk := range chunks {
		chunkCopy := chunk
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return errors.NewContextError(ctx.Err(), download.URL)
			}

			return e.downloadChunkWithRetries(ctx, download, chunkCopy)
		})
	}

	err := g.Wait()

	if err != nil {
		var downloadErr *errors.DownloadError
		if errors.As(err, &downloadErr) && downloadErr.Category == errors.CategoryContext {
			download.Status = common.StatusPaused
			if saveErr := e.saveDownload(download); saveErr != nil {
				log.Printf("Failed to save download: %s", saveErr)
			}
		} else {
			e.handleDownloadFailure(download, err)
		}
	} else {
		if err := e.finishDownload(download); err != nil {
			log.Printf("error finishing download: %s", err)
		}
	}
}

// downloadChunkWithRetries downloads a chunk with intelligent retry logic
func (e *Engine) downloadChunkWithRetries(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	err := e.downloadChunk(ctx, download, chunk)

	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}

	// Log the error with retry ability information
	log.Printf("Download error: %v (retryable: %v)", err, errors.IsRetryable(err))

	// Retry logic
	for chunk.RetryCount < download.Config.MaxRetries {
		// Reset the chunk for retry
		chunk.Reset()

		backoff := calculateBackoff(chunk.RetryCount, download.Config.RetryDelay)

		select {
		case <-time.After(backoff):
			log.Printf("Retrying chunk %s (attempt %d/%d)", chunk.ID, chunk.RetryCount+1, download.Config.MaxRetries)

			err = e.downloadChunk(ctx, download, chunk)
			if err == nil || errors.Is(err, context.Canceled) {
				return err
			}

			log.Printf("Download retry failed: %v (retryable: %v)", err, errors.IsRetryable(err))

			if !errors.IsRetryable(err) {
				return err
			}

		case <-ctx.Done():
			chunk.Status = common.StatusPaused
			return errors.NewContextError(ctx.Err(), fmt.Sprintf("chunk %s", chunk.ID))
		}
	}

	return fmt.Errorf("chunk %s failed after %d attempts: %w", chunk.ID, download.Config.MaxRetries, err)
}

// downloadChunk downloads a single chunk
func (e *Engine) downloadChunk(ctx context.Context, download *downloader.Download, chunk *chunk.Chunk) error {
	chunk.Status = common.StatusActive

	handler, err := e.protocolHandler.GetHandler(download.URL)
	if err != nil {
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return errors.NewNetworkError(err, download.URL, false)
	}

	conn, err := handler.CreateConnection(ctx, download.URL, chunk, download.Config)
	if err != nil {
		chunk.Status = common.StatusFailed
		chunk.Error = err
		return err // Already classified by protocol handler
	}

	e.connectionPool.RegisterConnection(conn)
	defer e.connectionPool.ReleaseConnection(conn)

	chunk.Connection = conn
	err = chunk.Download(ctx)

	if err != nil {
		// If we received a context cancellation, wrap it with our error system
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errors.NewContextError(err, download.URL)
		}
		// Other errors are already properly categorized
		return err
	}

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
	return pending
}

// handleDownloadFailure updates download state on failure
func (e *Engine) handleDownloadFailure(download *downloader.Download, err error) {
	download.Status = common.StatusFailed
	download.Error = err

	download.ErrorMessage = err.Error()

	if saveErr := e.saveDownload(download); saveErr != nil {
		log.Printf("failed to save download: %s", saveErr)
	}
}

func (e *Engine) finishDownload(download *downloader.Download) error {
	for _, chunk := range download.Chunks {
		if chunk.Status != common.StatusCompleted {
			err := fmt.Errorf("cannot finish download : chunk %s is in state %s", chunk.ID, chunk.Status)
			e.handleDownloadFailure(download, err)
			return err
		}
	}

	targetPath := filepath.Join(download.Config.Directory, download.Filename)
	if err := e.chunkManager.MergeChunks(download.Chunks, targetPath); err != nil {
		e.handleDownloadFailure(download, err)
		return fmt.Errorf("failed to merge chunks: %w", err)
	}

	download.Status = common.StatusCompleted
	download.EndTime = time.Now()

	if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
		log.Printf("failed to cleanup chunks: %s", err)
	}

	if err := e.saveDownload(download); err != nil {
		log.Printf("failed to save download: %s", err)
	}
	return nil
}

// restoreDownloadStates restores the state of downloads based on their status
func (e *Engine) restoreDownloadStates() error {
	var lastErr error

	for _, download := range e.downloads {
		switch download.Status {
		case common.StatusActive:
			// Downloads that were active should be paused on restart
			download.Status = common.StatusPaused
			if err := e.saveDownload(download); err != nil {
				lastErr = err
				log.Printf("Error setting download %s to paused: %v", download.ID, err)
			}

		case common.StatusQueued:
			download.Status = common.StatusQueued

		case common.StatusFailed, common.StatusCancelled:
			continue

		case common.StatusPaused:
			continue

		case common.StatusCompleted:
			outputPath := filepath.Join(download.Config.Directory, download.Filename)
			if _, err := os.Stat(outputPath); os.IsNotExist(err) {
				download.Status = common.StatusFailed
				download.Error = fmt.Errorf("output file missing")
				if err := e.saveDownload(download); err != nil {
					lastErr = err
					log.Printf("Error updating download %s status: %v", download.ID, err)
				}
			}
		}
	}

	return lastErr
}

// Shutdown gracefully stops the engine, saving all download states
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}

	log.Println("Starting engine shutdown...")

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
	log.Printf("Pausing %d active downloads...", len(activeDownloadIDs))
	for _, id := range activeDownloadIDs {
		if err := e.PauseDownload(id); err != nil {
			log.Printf("Error pausing download %s: %v", id, err)
		}
	}

	// Cancel the context to signal all operations to stop
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	// First stop services that might be starting new tasks
	log.Println("Stopping queue processor...")
	if e.queueProcessor != nil {
		e.queueProcessor.Stop()
	}

	log.Println("Stopping progress monitor...")
	if e.progressMonitor != nil {
		e.progressMonitor.Stop()
	}

	// Save all downloads
	log.Println("Saving download states...")
	e.saveAllDownloads()

	// Create a timeout context for the remaining shutdown operations
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Wait for tasks to complete with a timeout
	log.Println("Waiting for tasks to complete...")
	waitChan := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		log.Println("All tasks completed gracefully")
	case <-shutdownCtx.Done():
		log.Println("WARNING: Shutdown timed out, some tasks may not have completed")
	}

	// Now it's safe to close resources
	log.Println("Closing connection pool...")
	if e.connectionPool != nil {
		e.connectionPool.CloseAll()
	}

	if e.repository != nil {
		log.Println("Closing repository...")
		if err := e.repository.Close(); err != nil {
			log.Printf("Error closing repository: %v", err)
		}
	}

	// Close the progress channel last
	log.Println("Closing progress channel...")
	close(e.progressCh)

	log.Println("Engine shutdown complete")
	return nil
}

// startPeriodicSave starts a ticker to save download states periodically
func (e *Engine) startPeriodicSave(ctx context.Context) {
	interval := time.Duration(e.config.SaveInterval) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.saveAllDownloads()
		case <-ctx.Done():
			return
		}
	}
}

// saveAllDownloads saves the state of all downloads
func (e *Engine) saveAllDownloads() {
	if e.repository == nil {
		return
	}

	e.mu.RLock()
	downloads := make([]*downloader.Download, 0, len(e.downloads))
	for _, dl := range e.downloads {
		downloads = append(downloads, dl)
	}
	e.mu.RUnlock()

	for _, download := range e.downloads {
		if err := e.saveDownload(download); err != nil {
			log.Printf("Error saving download %s: %v", download.ID, err)
		}
	}
}

// saveDownload persists a download to the repository
func (e *Engine) saveDownload(download *downloader.Download) error {
	if e.repository == nil {
		return fmt.Errorf("repository not initialized")
	}

	download.PrepareForSerialization()

	return e.repository.Save(download)
}
