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
	ErrDownloadNotFound = errors.New("download not found")
	ErrInvalidURL       = errors.New("invalid URL")
	ErrDownloadExists   = errors.New("download already exists")
	ErrEngineNotRunning = errors.New("engine is not running")
)

// Engine orchestrates downloads, persistence and graceful shutdown.
type Engine struct {
	mu sync.RWMutex

	downloads       map[uuid.UUID]*downloader.Download
	protocolHandler *protocol.Handler
	connectionPool  *connection.Pool
	config          *Config
	repository      *repository.BboltRepository
	queueProcessor  *QueueProcessor

	stopCh chan struct{}

	wg            sync.WaitGroup
	saveStateChan chan *downloader.Download
	running       bool
}

// runTask spawns a helper goroutine tracked by the WaitGroup.
func (e *Engine) runTask(task func()) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		task()
	}()
}

// New builds an Engine with sane defaults and no persisted state loaded yet.
func New(cfg *Config) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create download directory: %w", err)
	}
	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}

	eng := &Engine{
		downloads:       make(map[uuid.UUID]*downloader.Download),
		protocolHandler: protocol.NewHandler(),
		connectionPool:  connection.NewPool(cfg.MaxConnectionsPerHost, 5*time.Minute),
		config:          cfg,
		stopCh:          make(chan struct{}),
		saveStateChan:   make(chan *downloader.Download, 5),
	}
	return eng, nil
}

// Init loads persisted downloads, starts background workers and marks the engine running.
func (e *Engine) Init() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.mu.Unlock()

	if err := e.initRepository(); err != nil {
		return err
	}
	if err := e.loadDownloads(); err != nil {
		return err
	}

	e.queueProcessor = NewQueueProcessor(e.config.MaxConcurrentDownloads, e.StartDownload)

	e.runTask(e.startPeriodicSave)

	e.runTask(func() {
		for {
			select {
			case <-e.stopCh:
				return
			case d := <-e.saveStateChan:
				_ = e.saveDownload(d)
			}
		}
	})

	for _, dl := range e.downloads {
		if dl.GetStatus() == common.StatusQueued {
			e.queueProcessor.Enqueue(dl.ID, dl.Config.Priority)
		}
	}

	logger.Infof("Engine initialised and running")
	return nil
}

func (e *Engine) initRepository() error {
	dir := e.config.ConfigDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		dir = filepath.Join(home, ".tdm")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	repo, err := repository.NewBboltRepository(filepath.Join(dir, "tdm.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	e.repository = repo
	return nil
}

func (e *Engine) loadDownloads() error {
	if e.repository == nil {
		return nil
	}
	dls, err := e.repository.FindAll()
	if err != nil {
		return fmt.Errorf("retrieve downloads: %w", err)
	}

	for _, dl := range dls {
		if err := dl.RestoreFromSerialization(e.protocolHandler, e.saveStateChan); err != nil {
			logger.Errorf("restore %s: %v", dl.ID, err)
			continue
		}
		e.downloads[dl.ID] = dl
	}
	return nil
}

func (e *Engine) AddDownload(url string, cfg *common.Config) (uuid.UUID, error) {
	if url == "" {
		return uuid.Nil, ErrInvalidURL
	}
	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return uuid.Nil, ErrEngineNotRunning
	}
	e.mu.RUnlock()

	e.mu.RLock()
	for _, dl := range e.downloads {
		if dl.URL == url {
			st := dl.GetStatus()
			if st == common.StatusActive || st == common.StatusPending || st == common.StatusPaused {
				e.mu.RUnlock()
				return uuid.Nil, ErrDownloadExists
			}
		}
	}
	e.mu.RUnlock()

	if cfg == nil {
		cfg = &common.Config{}
	}
	if cfg.Directory == "" {
		cfg.Directory = e.config.DownloadDir
	}
	if cfg.Connections <= 0 {
		cfg.Connections = e.config.MaxConnectionsPerDownload
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = e.config.MaxRetries
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Duration(e.config.RetryDelay) * time.Second
	}

	dl, err := downloader.NewDownload(context.Background(), url, e.protocolHandler, cfg, e.saveStateChan)
	if err != nil {
		return uuid.Nil, err
	}

	e.mu.Lock()
	e.downloads[dl.ID] = dl
	e.mu.Unlock()

	select {
	case e.saveStateChan <- dl:
	default:
	}

	if e.config.AutoStartDownloads {
		dl.SetStatus(common.StatusQueued)
		e.queueProcessor.Enqueue(dl.ID, dl.Config.Priority)
	}

	return dl.ID, nil
}

// StartDownload is called by the queue processor; blocks until the download finishes.
func (e *Engine) StartDownload(id uuid.UUID) error {
	dl, err := e.GetDownload(id)
	if err != nil {
		return err
	}

	select {
	case e.saveStateChan <- dl:
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-e.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	dl.Start(ctx, e.connectionPool)

	select {
	case e.saveStateChan <- dl:
	default:
	}

	return nil
}

func (e *Engine) PauseDownload(id uuid.UUID) error {
	dl, err := e.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Stop(common.StatusPaused, false)
	return nil
}

func (e *Engine) CancelDownload(id uuid.UUID, remove bool) error {
	dl, err := e.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Stop(common.StatusCancelled, remove)
	return nil
}

func (e *Engine) ResumeDownload(id uuid.UUID) error {
	dl, err := e.GetDownload(id)
	if err != nil {
		return err
	}
	if !dl.Resume(context.Background()) {
		return nil // nothing to do
	}
	dl.SetStatus(common.StatusQueued)
	e.queueProcessor.Enqueue(dl.ID, dl.Config.Priority)
	return nil
}

// RemoveDownload removes a download from the manager.
func (e *Engine) RemoveDownload(id uuid.UUID, removeFiles bool) error {
	logger.Infof("Removing download %s (removeFiles: %v)", id, removeFiles)

	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return ErrEngineNotRunning
	}
	e.mu.RUnlock()

	download, err := e.GetDownload(id)
	if err != nil {
		return fmt.Errorf("failed to get download: %w", err)
	}

	download.Remove() // stops & cleans chunks
	if err := e.deleteDownload(id); err != nil {
		return fmt.Errorf("failed to delete download: %w", err)
	}
	logger.Infof("Download %s removed successfully", id)
	return nil
}

// Shutdown broadcasts stop, pauses active downloads, waits for workers and flushes state.
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	close(e.stopCh)

	activeIDs := make([]uuid.UUID, 0)
	for id, dl := range e.downloads {
		if dl.GetStatus() == common.StatusActive {
			activeIDs = append(activeIDs, id)
		}
	}
	e.mu.Unlock()

	for _, id := range activeIDs {
		_ = e.PauseDownload(id)
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		logger.Warnf("shutdown timeout – workers still running")
	}

	e.saveAllDownloads()
	if e.connectionPool != nil {
		e.connectionPool.CloseAll()
	}
	if e.repository != nil {
		_ = e.repository.Close()
	}
	return nil
}

func (e *Engine) startPeriodicSave() {
	ticker := time.NewTicker(time.Duration(e.config.SaveInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.saveAllDownloads()
		}
	}
}

func (e *Engine) saveDownload(dl *downloader.Download) error {
	if e.repository == nil {
		return errors.New("repository not initialised")
	}
	return e.repository.Save(dl)
}

func (e *Engine) saveAllDownloads() {
	if e.repository == nil {
		return
	}
	e.mu.RLock()
	list := make([]*downloader.Download, 0, len(e.downloads))
	for _, dl := range e.downloads {
		list = append(list, dl)
	}
	e.mu.RUnlock()

	for _, dl := range list {
		_ = e.saveDownload(dl)
	}
}

// deleteDownload removes the download from the in‑memory map and repository.
func (e *Engine) deleteDownload(id uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.repository != nil {
		if err := e.repository.Delete(id); err != nil && !errors.Is(err, repository.ErrDownloadNotFound) {
			return fmt.Errorf("failed to delete from repository: %w", err)
		}
	}
	delete(e.downloads, id)
	return nil
}

func (e *Engine) GetDownload(id uuid.UUID) (*downloader.Download, error) {
	e.mu.RLock()
	dl, ok := e.downloads[id]
	e.mu.RUnlock()
	if !ok {
		return nil, ErrDownloadNotFound
	}
	return dl, nil
}

func (e *Engine) ListDownloads() []*downloader.Download {
	e.mu.RLock()
	out := make([]*downloader.Download, 0, len(e.downloads))
	for _, dl := range e.downloads {
		out = append(out, dl)
	}
	e.mu.RUnlock()
	return out
}
