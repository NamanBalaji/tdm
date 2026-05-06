// Package manager orchestrates download lifecycle, scheduling, and persistence.
package manager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/store"
)

var (
	ErrNotFound           = errors.New("download not found")
	ErrInvalidPriority    = errors.New("priority must be between 1 and 10")
	ErrNoDownloader       = errors.New("no downloader can handle this URL")
	ErrDownloaderNotFound = errors.New("no downloader registered for type")
	ErrShutdownTimeout    = errors.New("shutdown timed out waiting for downloads to stop")
)

// managedDownload wraps a Download with runtime state that only the Manager.
type managedDownload struct {
	download *download.Download
	cancel   context.CancelFunc
	tracker  *download.ProgressTracker
}

// Manager orchestrates downloads. It is the SOLE OWNER of all Download
// structs — no other goroutine reads or writes Download fields without
// going through the Manager.
type Manager struct {
	mu          sync.RWMutex
	downloads   map[uuid.UUID]*managedDownload
	downloaders []download.Downloader
	dlrByType   map[string]download.Downloader // type string → downloader (for reload)
	store       store.Store
	maxConc     int

	errors       chan download.DownloadError
	scheduleCh   chan struct{}
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	wg           sync.WaitGroup
}

func New(store store.Store, maxConcurrent int) *Manager {
	return &Manager{
		downloads:    make(map[uuid.UUID]*managedDownload),
		dlrByType:    make(map[string]download.Downloader),
		store:        store,
		maxConc:      maxConcurrent,
		errors:       make(chan download.DownloadError, 8),
		scheduleCh:   make(chan struct{}, 1),
		shutdownDone: make(chan struct{}),
	}
}

// Register adds a Downloader. Order matters: first CanHandle match wins.
func (m *Manager) Register(d download.Downloader) {
	m.downloaders = append(m.downloaders, d)
	m.dlrByType[d.Type()] = d
}

// Start loads persisted downloads from the store and starts the background run loop.
func (m *Manager) Start(ctx context.Context) error {
	downloads, err := m.store.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to load downloads: %w", err)
	}

	m.mu.Lock()
	for _, dl := range downloads {
		// Downloads that were Active when the app closed are reset to Paused
		if dl.Status == download.Active {
			dl.Status = download.Paused
		}
		// Downloads that were Queued/Pending are also paused (they'll be scheduled when resumed)
		if dl.Status == download.Queued || dl.Status == download.Pending {
			dl.Status = download.Paused
		}

		m.downloads[dl.ID] = &managedDownload{
			download: dl,
			tracker:  download.NewProgressTracker(5 * time.Second),
		}
	}
	m.mu.Unlock()

	m.wg.Add(1)

	go m.run(ctx)

	return nil
}

// AddDownload adds a new download from a URL.
func (m *Manager) AddDownload(ctx context.Context, url string, priority int) (uuid.UUID, error) {
	if priority < 1 || priority > 10 {
		return uuid.Nil, ErrInvalidPriority
	}

	// Find a Downloader that can handle this URL
	var dlr download.Downloader

	for _, d := range m.downloaders {
		if d.CanHandle(url) {
			dlr = d
			break
		}
	}

	if dlr == nil {
		return uuid.Nil, ErrNoDownloader
	}

	dl, err := dlr.Init(ctx, url, priority)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to initialize download: %w", err)
	}

	dl.CreatedAt = time.Now()

	if err := m.store.Save(ctx, dl); err != nil {
		return uuid.Nil, fmt.Errorf("failed to persist download: %w", err)
	}

	m.mu.Lock()
	m.downloads[dl.ID] = &managedDownload{
		download: dl,
		tracker:  download.NewProgressTracker(5 * time.Second),
	}
	m.mu.Unlock()

	m.requestReschedule()

	return dl.ID, nil
}

// PauseDownload pauses an active or queued download.
func (m *Manager) PauseDownload(ctx context.Context, id uuid.UUID) {
	m.mu.Lock()

	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return
	}

	if md.download.Status != download.Active && md.download.Status != download.Queued {
		m.mu.Unlock()
		return
	}

	md.download.Status = download.Paused
	if md.cancel != nil {
		md.cancel()
	}
	m.mu.Unlock()

	m.requestReschedule()
}

// ResumeDownload resumes a paused or failed download.
func (m *Manager) ResumeDownload(ctx context.Context, id uuid.UUID) {
	m.mu.Lock()

	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return
	}

	s := md.download.Status
	if s != download.Paused && s != download.Failed {
		m.mu.Unlock()
		return
	}

	md.download.Status = download.Queued
	md.tracker.Reset(md.download.Downloaded, md.download.TotalSize)
	m.mu.Unlock()

	m.requestReschedule()
}

// CancelDownload cancels a download.
func (m *Manager) CancelDownload(ctx context.Context, id uuid.UUID) {
	m.mu.Lock()

	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return
	}

	if md.download.Status.IsTerminal() {
		m.mu.Unlock()
		return
	}

	md.download.Status = download.Cancelled
	if md.cancel != nil {
		md.cancel()
	}
	m.mu.Unlock()

	m.requestReschedule()
}

// RemoveDownload cancels and deletes a download completely.
func (m *Manager) RemoveDownload(ctx context.Context, id uuid.UUID) {
	m.mu.Lock()

	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return
	}

	// Cancel if running
	md.download.Status = download.Cancelled
	if md.cancel != nil {
		md.cancel()
	}

	delete(m.downloads, id)
	m.mu.Unlock()

	dlr := m.findDownloader(md.download.Type)
	if dlr != nil {
		if err := dlr.Remove(md.download); err != nil {
			logger.Errorf("failed to remove download files: %v", err)
		}
	}

	if err := m.store.Delete(ctx, id); err != nil {
		logger.Errorf("failed to delete download from store: %v", err)
	}

	m.requestReschedule()
}

// GetAllDownloads returns a snapshot of all downloads for the TUI.
// Uses RLock since it only reads — multiple TUI refreshes don't block each other.
func (m *Manager) GetAllDownloads() []download.DownloadInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]download.DownloadInfo, 0, len(m.downloads))
	for _, md := range m.downloads {
		info := download.DownloadInfo{
			ID:       md.download.ID,
			Filename: md.download.Filename,
			Status:   md.download.Status,
			Priority: md.download.Priority,
		}

		if md.download.Status == download.Active {
			info.Progress = md.tracker.Snapshot()
		} else {
			// For non-active downloads, show static progress
			pct := 0.0
			if md.download.TotalSize > 0 {
				pct = float64(md.download.Downloaded) / float64(md.download.TotalSize) * 100
			}

			if md.download.Status == download.Completed {
				pct = 100
			}

			info.Progress = download.Progress{
				TotalSize:  md.download.TotalSize,
				Downloaded: md.download.Downloaded,
				Percentage: pct,
			}
		}

		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}

		return result[i].ID.String() < result[j].ID.String()
	})

	return result
}

// GetErrors returns the error channel for monitoring.
func (m *Manager) GetErrors() <-chan download.DownloadError {
	return m.errors
}

// Shutdown gracefully stops all downloads and waits for goroutines to finish.
func (m *Manager) Shutdown(ctx context.Context) error {
	var err error

	m.shutdownOnce.Do(func() {
		// Pause all active downloads
		m.mu.Lock()
		for _, md := range m.downloads {
			if md.download.Status == download.Active {
				md.download.Status = download.Paused
				if md.cancel != nil {
					md.cancel()
				}
			}
		}
		m.mu.Unlock()

		done := make(chan struct{})

		go func() {
			m.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
			err = ErrShutdownTimeout
		}

		m.saveAll(ctx)

		close(m.errors)
		close(m.shutdownDone)
	})

	return err
}

// Wait blocks until shutdown is complete.
func (m *Manager) Wait() {
	<-m.shutdownDone
}

// run is the Manager's background loop. It owns the context on its stack
// (never stored in the struct) and handles scheduling + periodic persistence.
func (m *Manager) run(ctx context.Context) {
	defer m.wg.Done()

	persistTicker := time.NewTicker(1 * time.Second)
	defer persistTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.scheduleCh:
			m.doSchedule(ctx)
		case <-persistTicker.C:
			m.saveAll(ctx)
		}
	}
}

// requestReschedule signals the run loop to re-evaluate scheduling.
func (m *Manager) requestReschedule() {
	select {
	case m.scheduleCh <- struct{}{}:
	default: // already one pending, no need to queue another
	}
}

// doSchedule evaluates which downloads should be active and starts/pauses accordingly.
func (m *Manager) doSchedule(ctx context.Context) {
	m.mu.Lock()

	decision := schedule(m.downloads, m.maxConc)

	for _, md := range decision.toPause {
		md.download.Status = download.Queued
		if md.cancel != nil {
			md.cancel()
		}
	}

	for _, md := range decision.toStart {
		md.download.Status = download.Active
		md.download.StartTime = time.Now()

		runCtx, cancel := context.WithCancel(ctx) // child of the run loop's ctx
		md.cancel = cancel

		m.wg.Add(1)

		go m.runDownload(ctx, runCtx, md)
	}

	m.mu.Unlock()
}

func (m *Manager) runDownload(managerCtx, dlCtx context.Context, md *managedDownload) {
	defer m.wg.Done()

	dlr := m.findDownloader(md.download.Type)
	if dlr == nil {
		m.mu.Lock()
		md.download.Status = download.Failed
		m.mu.Unlock()

		m.sendError(md.download.ID, fmt.Errorf("%w: %q", ErrDownloaderNotFound, md.download.Type))

		return
	}

	err := dlr.Start(dlCtx, md.download, func(downloaded, totalSize int64) {
		md.tracker.Update(downloaded, totalSize)
	})

	m.handleCompletion(managerCtx, md, err)
}

func (m *Manager) handleCompletion(ctx context.Context, md *managedDownload, err error) {
	m.mu.Lock()

	if err == nil {
		md.download.Status = download.Completed
		md.download.EndTime = time.Now()
		md.download.Downloaded = md.download.TotalSize
	} else if errors.Is(err, context.Canceled) {
		snap := md.tracker.Snapshot()
		md.download.Downloaded = snap.Downloaded
	} else {
		md.download.Status = download.Failed
		snap := md.tracker.Snapshot()
		md.download.Downloaded = snap.Downloaded
		m.sendError(md.download.ID, err)
	}

	md.cancel = nil
	dlCopy := *md.download
	m.mu.Unlock()

	if saveErr := m.store.Save(ctx, &dlCopy); saveErr != nil {
		logger.Errorf("failed to save download after completion: %v", saveErr)
	}

	m.requestReschedule()
}

// saveAll persists all downloads to the store.
func (m *Manager) saveAll(ctx context.Context) {
	m.mu.RLock()

	toSave := make([]*download.Download, 0, len(m.downloads))
	for _, md := range m.downloads {
		dlCopy := *md.download
		toSave = append(toSave, &dlCopy)
	}

	m.mu.RUnlock()

	for _, dl := range toSave {
		if err := m.store.Save(ctx, dl); err != nil {
			logger.Errorf("failed to persist download %s: %v", dl.ID, err)
		}
	}
}

// sendError sends an error on the errors channel without blocking.
func (m *Manager) sendError(id uuid.UUID, err error) {
	select {
	case m.errors <- download.DownloadError{ID: id, Error: err}:
	default:
		logger.Errorf("error channel full, dropping error for %s: %v", id, err)
	}
}

// findDownloader finds the registered Downloader for the given type string.
func (m *Manager) findDownloader(dlType string) download.Downloader {
	return m.dlrByType[dlType]
}
