package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/protocol"
	"github.com/google/uuid"
)

var (
	// ErrDownloadNotFound is returned when a download cannot be found
	ErrDownloadNotFound = errors.New("download not found")

	// ErrInvalidURL is returned for malformed URLs
	ErrInvalidURL = errors.New("invalid URL")

	// ErrDownloadExists is returned when trying to add a duplicate download
	ErrDownloadExists = errors.New("download already exists")
)

// Config contains download manager configuration
type Config struct {
	DownloadDir               string
	MaxConcurrentDownloads    int
	MaxConnectionsPerDownload int
	ChunkSize                 int64
	UserAgent                 string
	TempDir                   string
}

type Engine struct {
	mu sync.RWMutex

	downloads       map[uuid.UUID]*downloader.Download
	protocolHandler *protocol.Handler
	connectionPool  *connection.Pool
	chunkManager    *chunk.Manager
	config          *Config

	// Context for downloads
	ctx        context.Context
	cancelFunc context.CancelFunc

	// Channel for global progress updates
	progressCh chan common.Progress

	// Is the manager running
	running bool
}

func DefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	downloadDir := filepath.Join(homeDir, "Downloads")

	return &Config{
		DownloadDir:               downloadDir,
		MaxConcurrentDownloads:    3,
		MaxConnectionsPerDownload: 8,
		ChunkSize:                 4 * 1024 * 1024, // 4MB
		UserAgent:                 "TDM/1.0",
		TempDir:                   filepath.Join(os.TempDir(), "tdm"),
	}
}

func New(config *Config) (*Engine, error) {
	if config == nil {
		config = DefaultConfig()
	}

	protocolHandler := protocol.NewHandler()
	connectionPool := connection.NewPool(10, 5*time.Minute)
	chunkManager, err := chunk.NewManager(config.TempDir)
	if err != nil {
		return nil, err
	}

	engine := &Engine{
		downloads:       make(map[uuid.UUID]*downloader.Download),
		protocolHandler: protocolHandler,
		connectionPool:  connectionPool,
		chunkManager:    chunkManager,
		config:          config,
		progressCh:      make(chan common.Progress, 1000),
	}

	engine.ctx, engine.cancelFunc = context.WithCancel(context.Background())

	return engine, nil
}

func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return nil
	}

	// load downloads
	// start progress monitoring

	e.running = true
	return nil
}

func (e *Engine) Shutdown() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.cancelFunc()
	// save all downloads
	e.connectionPool.CloseAll()
	close(e.progressCh)
	e.running = false
	return nil
}

func (e *Engine) AddDownload(url string, opts *downloader.DownloadOptions) (uuid.UUID, error) {
	if url == "" {
		return uuid.Nil, ErrInvalidURL
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicate URL
	for _, download := range e.downloads {
		if download.URL == url && download.Status != common.StatusCompleted && download.Status != common.StatusFailed {
			return uuid.Nil, ErrDownloadExists
		}
	}

	if opts.Directory == "" {
		opts.Directory = e.config.DownloadDir
	}
	if opts.Connections <= 0 {
		opts.Connections = e.config.MaxConnectionsPerDownload
	}
	opts.URL = url

	info, err := e.protocolHandler.Initialize(url, opts)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to initialize download: %w", err)
	}

	filename := opts.Filename
	if filename == "" {
		filename = info.Filename
	}

	// Ensure directory exists
	if err := os.MkdirAll(opts.Directory, 0o755); err != nil {
		return uuid.Nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create download object
	download := downloader.NewDownload(url, filename, opts)
	download.TotalSize = info.TotalSize

	// Create chunks
	chunks, err := e.chunkManager.CreateChunks(download.ID, info.TotalSize, info.SupportsRanges, opts.Connections, download.AddProgress)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create chunks: %w", err)
	}
	download.Chunks = chunks

	e.downloads[download.ID] = download

	// save download

	return download.ID, nil
}

// GetDownload retrieves a download by ID
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
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if download exists
	download, ok := e.downloads[id]
	if !ok {
		return ErrDownloadNotFound
	}

	// Cancel download if active
	if download.Status == common.StatusActive {
		err := e.CancelDownload(id, removeFiles)
		if err != nil {
			return err
		}
	}

	// Remove temporary files
	if removeFiles {
		// Remove chunk files
		err := e.chunkManager.CleanupChunks(download.Chunks)
		if err != nil {
			return err
		}

		// Remove output file if it exists
		outputPath := filepath.Join(download.Options.Directory, download.Filename)
		if _, err := os.Stat(outputPath); err == nil {
			if err := os.Remove(outputPath); err != nil {
				log.Printf("Warning: Failed to remove output file: %v", err)
			}
		}
	}

	// Remove from downloads map
	delete(e.downloads, id)

	return nil
}

// GetGlobalStats returns global download statistics
func (e *Engine) GetGlobalStats() GlobalStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := GlobalStats{
		MaxConcurrent: e.config.MaxConcurrentDownloads,
	}

	// Count downloads by status
	for _, download := range e.downloads {
		switch download.Status {
		case common.StatusActive:
			stats.ActiveDownloads++
			stats.CurrentConcurrent++
		case common.StatusQueued:
			stats.QueuedDownloads++
		case common.StatusCompleted:
			stats.CompletedDownloads++
		case common.StatusFailed:
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

	// Calculate average speed
	if stats.ActiveDownloads > 0 {
		stats.AverageSpeed = stats.CurrentSpeed / int64(stats.ActiveDownloads)
	}

	return stats
}

// PauseDownload pauses an active download
func (e *Engine) PauseDownload(id uuid.UUID) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	// Can only pause active downloads
	if download.Status != common.StatusActive {
		return nil
	}

	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusPaused

	// Save updated state

	return nil
}

// ResumeDownload resumes a paused download
func (e *Engine) ResumeDownload(ctx context.Context, id uuid.UUID) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	// Can only resume paused downloads
	if download.Status != common.StatusPaused {
		return nil
	}

	// Create new context with cancellation
	downloadCtx, cancelFunc := context.WithCancel(ctx)
	download.SetContext(downloadCtx, cancelFunc)

	// Update status
	download.Status = common.StatusActive

	// Save updated state

	go e.processDownload(download)

	return nil
}

// CancelDownload cancels a download
func (e *Engine) CancelDownload(id uuid.UUID, removeFiles bool) error {
	download, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	if download.Status == common.StatusCompleted {
		return nil
	}

	// Cancel the download operation
	if download.CancelFunc() != nil {
		download.CancelFunc()()
	}

	download.Status = common.StatusFailed

	if removeFiles {
		if err := e.chunkManager.CleanupChunks(download.Chunks); err != nil {
			log.Printf("Warning: Failed to clean up chunks: %v", err)
		}

		// delete files
	}

	// Save updated state

	return nil
}
