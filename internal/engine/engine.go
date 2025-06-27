package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/worker"
)

var (
	ErrWorkerNotFound    = errors.New("worker not found")
	ErrInvalidPriority   = errors.New("priority must be between 1 and 10")
	ErrDownloadNotPaused = errors.New("download is not paused")
)

// DownloadError represents an error for a specific download.
type DownloadError struct {
	ID    uuid.UUID
	Error error
}

// Engine manages multiple download workers.
type Engine struct {
	mu            sync.RWMutex
	repo          *repository.BboltRepository
	maxConcurrent int
	workers       map[uuid.UUID]worker.Worker
	queue         *PriorityQueue
	activeCount   int
	shutdownOnce  sync.Once
	shutdownDone  chan struct{}
	errors        chan DownloadError
	schedulerStop chan struct{}
}

// NewEngine creates a new download engine.
func NewEngine(repo *repository.BboltRepository, maxConcurrent int) *Engine {
	return &Engine{
		repo:          repo,
		maxConcurrent: maxConcurrent,
		workers:       make(map[uuid.UUID]worker.Worker),
		queue:         NewPriorityQueue(),
		shutdownDone:  make(chan struct{}),
		errors:        make(chan DownloadError),
		schedulerStop: make(chan struct{}),
	}
}

// Start initializes the engine and loads existing downloads.
func (e *Engine) Start(ctx context.Context) error {
	downloads, err := e.repo.GetAll()
	if err != nil {
		return fmt.Errorf("failed to load downloads: %w", err)
	}

	for _, dl := range downloads {
		switch dl.Type {
		case "http":
			var download http.Download
			err := json.Unmarshal(dl.Data, &download)
			if err != nil {
				logger.Errorf("Failed to unmarshal download: %v", err)
				continue
			}

			if download.Status == status.Active {
				download.Status = status.Paused
			}

			w, err := http.New(ctx, download.URL, &download, e.repo)
			if err != nil {
				logger.Errorf("Failed to create worker for download %s: %v", download.Id, err)
				continue
			}

			e.addWorker(w)

			go e.monitorWorker(w)
		}
	}

	go e.scheduler(ctx)

	return nil
}

// addWorker adds a worker to the engine.
func (e *Engine) addWorker(w worker.Worker) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.workers[w.GetID()] = w
}

// scheduler manages the download queue and active downloads.
func (e *Engine) scheduler(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.schedulerStop:
			return
		case <-ticker.C:
			e.processQueue(ctx)
		}
	}
}

// monitorWorker monitors a single worker for completion.
func (e *Engine) monitorWorker(w worker.Worker) {
	select {
	case err := <-w.Done():
		e.handleWorkerDone(w.GetID(), err)
	}
}

// handleWorkerDone handles cleanup when a worker finishes.
func (e *Engine) handleWorkerDone(workerID uuid.UUID, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	w, exists := e.workers[workerID]
	if !exists {
		return
	}

	s := w.GetStatus()

	if s == status.Completed || s == status.Failed || s == status.Cancelled {
		if e.activeCount > 0 {
			e.activeCount--
		}

		e.queue.Remove(workerID)

		// Send error if any
		if err != nil {
			select {
			case e.errors <- DownloadError{ID: workerID, Error: err}:
			default:
				logger.Errorf("Download %s failed: %v", workerID, err)
			}
		}
	}
}

func (e *Engine) processQueue(ctx context.Context) {
	for {
		e.mu.Lock()

		if e.queue.Size() == 0 {
			e.mu.Unlock()
			return
		}

		item := e.queue.PopHighest()
		if item == nil {
			e.mu.Unlock()
			return
		}

		w, ok := e.workers[item.ID]
		if !ok || w.GetStatus() == status.Active {
			e.mu.Unlock()
			continue
		}

		if e.activeCount < e.maxConcurrent {
			e.activeCount++
			e.mu.Unlock()

			err := w.Start(ctx)
			if err != nil && !errors.Is(err, http.ErrAlreadyStarted) {
				logger.Errorf("Failed to start download %s: %v", item.ID, err)
				e.mu.Lock()
				e.activeCount--
				e.mu.Unlock()
			} else {
				go e.monitorWorker(w)
			}

			continue
		}

		var lowest worker.Worker

		lowPrio := 11
		for _, aw := range e.workers {
			if aw.GetStatus() == status.Active && aw.GetPriority() < lowPrio {
				lowest, lowPrio = aw, aw.GetPriority()
			}
		}

		// No one weaker → re-queue and quit
		if lowest == nil || lowPrio >= item.Priority {
			e.queue.Add(item.ID, item.Priority)
			e.mu.Unlock()

			return
		}

		e.activeCount--
		e.mu.Unlock()

		_ = lowest.Pause()

		w.Queue()

		e.mu.Lock()
		e.queue.Add(lowest.GetID(), lowPrio)
		e.activeCount++
		e.mu.Unlock()

		err := w.Start(ctx)
		if err != nil && !errors.Is(err, http.ErrAlreadyStarted) {
			logger.Errorf("Failed to start download %s: %v", item.ID, err)
			e.mu.Lock()
			e.activeCount--
			e.mu.Unlock()
		} else {
			go e.monitorWorker(w)
		}
	}
}

// AddDownload adds a new download to the engine.
func (e *Engine) AddDownload(ctx context.Context, url string, priority int, opts ...http.ConfigOption) (uuid.UUID, error) {
	if priority < 1 || priority > 10 {
		return uuid.Nil, ErrInvalidPriority
	}

	opts = append(opts, http.WithPriority(priority))

	w, err := worker.GetWorker(ctx, url, e.repo)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to create worker: %w", err)
	}

	id := w.GetID()

	e.mu.Lock()
	e.workers[id] = w
	e.queue.Add(id, priority)
	e.mu.Unlock()

	go e.monitorWorker(w)

	e.processQueue(ctx)

	return id, nil
}

// PauseDownload pauses a download.
func (e *Engine) PauseDownload(id uuid.UUID) error {
	e.mu.RLock()
	w, exists := e.workers[id]
	e.mu.RUnlock()

	if !exists {
		return ErrWorkerNotFound
	}

	err := w.Pause()
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.queue.Remove(id)

	if w.GetStatus() == status.Paused && e.activeCount > 0 {
		e.activeCount--
	}

	e.mu.Unlock()

	return nil
}

// ResumeDownload resumes a paused download.
func (e *Engine) ResumeDownload(ctx context.Context, id uuid.UUID) error {
	e.mu.RLock()
	w, exists := e.workers[id]
	e.mu.RUnlock()

	if !exists {
		return ErrWorkerNotFound
	}

	s := w.GetStatus()
	if s != status.Paused && s != status.Failed {
		return ErrDownloadNotPaused
	}

	e.mu.Lock()
	e.queue.Add(id, w.GetPriority())
	w.Queue()
	e.mu.Unlock()

	e.processQueue(ctx)

	return nil
}

// CancelDownload cancels a download.
func (e *Engine) CancelDownload(id uuid.UUID) error {
	e.mu.RLock()
	w, exists := e.workers[id]
	e.mu.RUnlock()

	if !exists {
		return ErrWorkerNotFound
	}

	err := w.Cancel()
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.queue.Remove(id)

	if w.GetStatus() == status.Cancelled && e.activeCount > 0 {
		e.activeCount--
	}

	e.mu.Unlock()

	return nil
}

// RemoveDownload removes a download completely.
func (e *Engine) RemoveDownload(id uuid.UUID) error {
	e.mu.Lock()

	w, exists := e.workers[id]
	if !exists {
		e.mu.Unlock()
		return ErrWorkerNotFound
	}

	delete(e.workers, id)
	e.queue.Remove(id)

	s := w.GetStatus()
	if s == status.Active && e.activeCount > 0 {
		e.activeCount--
	}

	e.mu.Unlock()

	return w.Remove()
}

// GetProgress returns the progress of a download.
func (e *Engine) GetProgress(id uuid.UUID) (progress.Progress, error) {
	e.mu.RLock()
	w, exists := e.workers[id]
	e.mu.RUnlock()

	if !exists {
		return nil, ErrWorkerNotFound
	}

	return w.Progress(), nil
}

// GetAllDownloads returns info about all downloads.
func (e *Engine) GetAllDownloads() []DownloadInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	downloads := make([]DownloadInfo, 0, len(e.workers))
	for id, w := range e.workers {
		downloads = append(downloads, DownloadInfo{
			ID:       id,
			Filename: w.GetFilename(),
			Status:   w.GetStatus(),
			Priority: w.GetPriority(),
			Progress: w.Progress(),
		})
	}

	sort.Slice(downloads, func(i, j int) bool {
		if downloads[i].Priority != downloads[j].Priority {
			return downloads[i].Priority > downloads[j].Priority
		}

		return downloads[i].ID.String() < downloads[j].ID.String()
	})

	return downloads
}

// GetErrors returns the error channel for monitoring download errors.
func (e *Engine) GetErrors() <-chan DownloadError {
	return e.errors
}

// Shutdown gracefully shuts down the engine.
func (e *Engine) Shutdown(ctx context.Context) error {
	var shutdownErr error

	e.shutdownOnce.Do(func() {
		close(e.schedulerStop)

		e.mu.Lock()

		var wg sync.WaitGroup

		for _, w := range e.workers {
			if w.GetStatus() == status.Active {
				wg.Add(1)

				go func(worker worker.Worker) {
					defer wg.Done()
					err := worker.Pause()

					if err != nil {
						logger.Errorf("Failed to pause download %s: %v", worker.GetID(), err)
					}
				}(w)
			}
		}

		e.mu.Unlock()

		done := make(chan struct{})

		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		case <-time.After(10 * time.Second):
			shutdownErr = errors.New("timeout waiting for downloads to pause")
		}

		close(e.errors)
		close(e.shutdownDone)
	})

	return shutdownErr
}

// Wait waits for the engine to shut down.
func (e *Engine) Wait() {
	<-e.shutdownDone
}

// DownloadInfo contains information about a download.
type DownloadInfo struct {
	ID       uuid.UUID
	Filename string
	Status   status.Status
	Priority int
	Progress progress.Progress
}
