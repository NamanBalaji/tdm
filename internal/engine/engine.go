package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/protocol"
	"github.com/NamanBalaji/tdm/internal/repository"
)

var (
	// ErrDownloadNotFound is returned when a download cannot be found.
	ErrDownloadNotFound = errors.New("download not found")

	// ErrInvalidURL is returned for malformed URLs.
	ErrInvalidURL = errors.New("invalid URL")

	// ErrDownloadExists is returned when trying to add a duplicate download.
	ErrDownloadExists = errors.New("download already exists")

	// ErrEngineNotRunning is returned when an operation requires the engine to be running.
	ErrEngineNotRunning = errors.New("engine is not running")
)

type Engine struct {
	mu sync.RWMutex

	downloads       map[uuid.UUID]*downloader.Download
	protocolHandler *protocol.Handler
	connectionPool  *connection.Pool
	config          *Config
	repository      *repository.BboltRepository
	queueProcessor  *QueueProcessor

	ctx           context.Context
	cancelFunc    context.CancelFunc
	wg            sync.WaitGroup
	saveStateChan chan *downloader.Download
	running       bool
}

// runTask runs a function in a goroutine tracked by the WaitGroup.
func (e *Engine) runTask(task func()) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		task()
	}()
}

// New creates a new Engine instance.
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
	connectionPool := connection.NewPool(16, 5*time.Minute)

	ctx, cancelFunc := context.WithCancel(context.Background())

	engine := &Engine{
		downloads:       make(map[uuid.UUID]*downloader.Download),
		protocolHandler: protocolHandler,
		connectionPool:  connectionPool,
		config:          config,
		ctx:             ctx,
		cancelFunc:      cancelFunc,
		saveStateChan:   make(chan *downloader.Download, 5),
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
	e.queueProcessor = NewQueueProcessor(e.config.MaxConcurrentDownloads, e.StartDownload)

	e.runTask(func() {
		logger.Debugf("Starting queue processor")
		e.queueProcessor.Start(e.ctx)
	})

	e.runTask(func() {
		logger.Debugf("Starting periodic save with interval %d seconds", e.config.SaveInterval)
		e.startPeriodicSave(e.ctx)
	})

	e.runTask(func() {
		for {
			select {
			case <-e.ctx.Done():
				return
			case d := <-e.saveStateChan:
				err := e.saveDownload(d)
				if err != nil {
					logger.Errorf("Failed to save download %s: %v", d, err)
				}
			}
		}
	})

	queuedCount := 0
	for _, download := range e.downloads {
		if download.GetStatus() == common.StatusQueued {
			logger.Debugf("Enqueueing previously queued download: %s", download.ID)
			download.SetStatus(common.StatusQueued)
			e.queueProcessor.EnqueueDownload(download.ID, download.Config.Priority)
			queuedCount++
		}
	}
	logger.Debugf("Enqueued %d previously queued downloads", queuedCount)

	e.running = true
	logger.Infof("Engine initialized and running")

	return nil
}

// initRepository initializes the download repository.
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

// loadDownloads loads existing downloads from the repository.
func (e *Engine) loadDownloads() error {
	logger.Debugf("Loading downloads from repository")

	downloads, err := e.repository.FindAll()
	if err != nil {
		logger.Errorf("Failed to retrieve downloads from repository: %v", err)
		return fmt.Errorf("failed to retrieve downloads: %w", err)
	}

	for _, download := range downloads {
		logger.Debugf("Restoring download: %s, URL: %s", download.ID, download.URL)
		if err := download.RestoreFromSerialization(e.ctx, e.protocolHandler, e.saveStateChan); err != nil {
			logger.Errorf("Failed to restore download %s: %v", download.ID, err)
			continue
		}

		e.downloads[download.ID] = download
		logger.Debugf("Download %s restored with status: %s", download.ID, download.GetStatus())
	}

	logger.Infof("Loaded %d download(s) from repository", len(e.downloads))
	return nil
}

// AddDownload adds a new download to the Engine.
func (e *Engine) AddDownload(url string, config *common.Config) (uuid.UUID, error) {
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
	// Check for duplicate URL
	for _, download := range e.downloads {
		status := download.GetStatus()
		if download.URL == url && (status == common.StatusActive || status == common.StatusPending || status == common.StatusPaused) {
			logger.Warnf("Download already exists for URL: %s", url)
			e.mu.Unlock()

			return uuid.Nil, ErrDownloadExists
		}
	}
	e.mu.Unlock()

	dConfig := config
	if dConfig == nil {
		logger.Debugf("No config provided, using default download config")
		dConfig = &common.Config{}
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

	download, err := downloader.NewDownload(e.ctx, url, e.protocolHandler, dConfig, e.saveStateChan)
	if err != nil {
		logger.Errorf("Failed to create download: %v", err)
		return uuid.Nil, fmt.Errorf("failed to create download: %w", err)
	}
	e.mu.Lock()
	e.downloads[download.ID] = download
	e.mu.Unlock()

	if err := e.repository.Save(download); err != nil {
		logger.Errorf("Failed to save download to repository: %v", err)
		e.mu.Lock()
		delete(e.downloads, download.ID)
		e.mu.Unlock()
		return uuid.Nil, fmt.Errorf("failed to save download to repository: %w", err)
	}

	if e.config.AutoStartDownloads {
		logger.Debugf("Auto-starting download %s", download.ID)
		download.SetStatus(common.StatusQueued)
		e.queueProcessor.EnqueueDownload(download.ID, download.Config.Priority)
	}

	logger.Infof("Download added successfully with ID: %s", download.ID)

	return download.ID, nil
}

// GetDownload retrieves a download by ID string.
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

// ListDownloads returns all downloads.
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

// RemoveDownload removes a download from the manager.
func (e *Engine) RemoveDownload(id uuid.UUID, removeFiles bool) error {
	logger.Infof("Removing download %s (removeFiles: %v)", id, removeFiles)

	if !e.running {
		logger.Errorf("Cannot remove download, engine is not running")
		return ErrEngineNotRunning
	}

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return fmt.Errorf("failed to get download: %w", err)
	}

	download.Remove()
	if err := e.deleteDownload(id); err != nil {
		logger.Errorf("Failed to delete download %s from repository: %v", id, err)
		return fmt.Errorf("failed to delete download: %w", err)
	}
	logger.Infof("Download %s removed successfully", id)
	return nil
}

// deleteDownload deletes a download from the engine and repository.
func (e *Engine) deleteDownload(id uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.repository == nil {
		logger.Debugf("Repository not initialized, skipping download deletion from database")
		delete(e.downloads, id)
		return nil
	}

	logger.Debugf("Deleting download %s from repository", id)
	if err := e.repository.Delete(id); err != nil && !errors.Is(err, repository.ErrDownloadNotFound) {
		logger.Errorf("Failed to delete download from repository: %v", err)
		return fmt.Errorf("failed to delete download from repository: %w", err)
	}

	delete(e.downloads, id)
	return nil
}

// GetGlobalStats returns global download statistics.
func (e *Engine) GetGlobalStats() common.GlobalStats {
	logger.Debugf("Getting global download stats")

	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := common.GlobalStats{
		MaxConcurrent: e.config.MaxConcurrentDownloads,
	}

	for _, download := range e.downloads {
		switch download.GetStatus() {
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
		default:
			logger.Warnf("Unknown download status: %s", download.GetStatus())
		}

		stats.TotalDownloaded += download.GetDownloaded()

		if download.GetStatus() == common.StatusActive {
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

// PauseDownload pauses an active download.
func (e *Engine) PauseDownload(id uuid.UUID) error {
	logger.Infof("Pausing download: %s", id)

	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}
	download.Stop(common.StatusPaused, false)
	logger.Infof("Download %s paused successfully", id)

	return nil
}

// CancelDownload cancels a download.
func (e *Engine) CancelDownload(id uuid.UUID, removeFiles bool) error {
	logger.Infof("Cancelling download %s (removeFiles: %v)", id, removeFiles)

	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}
	download.Stop(common.StatusCancelled, true)
	logger.Infof("Download %s cancelled successfully", id)

	return nil
}

// ResumeDownload resumes a paused download.
func (e *Engine) ResumeDownload(id uuid.UUID) error {
	logger.Infof("Resuming download: %s", id)

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return fmt.Errorf("failed to get download: %w", err)
	}

	if !download.Resume(e.ctx) {
		return nil
	}

	logger.Debugf("Enqueueing download %s for resumption", id)
	download.SetStatus(common.StatusQueued)
	e.queueProcessor.EnqueueDownload(download.ID, download.Config.Priority)
	logger.Infof("Download %s resumed successfully", id)

	return nil
}

// StartDownload initiates a download.
func (e *Engine) StartDownload(id uuid.UUID) error {
	logger.Infof("Starting download: %s", id)

	download, err := e.GetDownload(id)
	if err != nil {
		logger.Errorf("Failed to get download %s: %v", id, err)
		return err
	}

	if err := e.saveDownload(download); err != nil {
		logger.Errorf("Failed to save download %s after starting: %v", id, err)
		return fmt.Errorf("failed to save download: %w", err)
	}

	completed := make(chan uuid.UUID)
	e.runTask(func() {
		e.queueProcessor.NotifyDownloadCompletion(<-completed)
	})

	e.runTask(func() {
		logger.Debugf("Processing download %s in background", id)
		download.Start(e.connectionPool, completed)
	})

	logger.Infof("Download %s started successfully", id)
	return nil
}

// Shutdown gracefully stops the engine, saving all download states.
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
		if download.GetStatus() == common.StatusActive {
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

	logger.Infof("Stopping queue processor...")
	if e.queueProcessor != nil {
		e.queueProcessor.Stop()
	}

	logger.Debugf("Cancelling engine context")
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

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

	logger.Infof("Saving download states...")
	e.saveAllDownloads()

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

	logger.Infof("Engine shutdown complete")
	return nil
}

// startPeriodicSave starts a ticker to save download states periodically.
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

// saveAllDownloads saves the state of all downloads.
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
			continue
		}
		saveCount++
	}
	logger.Debugf("Successfully saved %d downloads", saveCount)
}

// saveDownload persists a download to the repository.
func (e *Engine) saveDownload(download *downloader.Download) error {
	if e.repository == nil {
		logger.Errorf("Cannot save download %s: repository not initialized", download.ID)
		return errors.New("repository not initialized")
	}

	logger.Debugf("Saving download %s to repository", download.ID)
	return e.repository.Save(download)
}
