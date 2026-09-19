// Package manager orchestrates download lifecycle, scheduling, and persistence.
package manager

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/store"
)

var (
	ErrNotFound           = errors.New("download not found")
	ErrInvalidPriority    = errors.New("priority must be between 1 and 10")
	ErrNoDownloader       = errors.New("no downloader can handle this URL")
	ErrDownloaderNotFound = errors.New("no downloader registered for type")
	ErrShutdownTimeout    = errors.New("shutdown timed out waiting for downloads to stop")
	ErrDuplicateURL       = errors.New("download with this URL already exists")
)

// managedDownload wraps a Download with runtime state that only the Manager.
type managedDownload struct {
	download *download.Download
	cancel   context.CancelFunc
	tracker  *download.ProgressTracker
	dirty    bool // has unsaved changes; cleared by the persist loop
	running  bool // a Start goroutine is live; cleared by handleCompletion
}

// Manager orchestrates downloads. It is the SOLE OWNER of all Download
// structs — no other goroutine reads or writes Download fields without
// going through the Manager.
type Manager struct {
	mu          sync.RWMutex
	downloads   map[uuid.UUID]*managedDownload
	pendingURLs map[string]struct{} // URLs whose AddDownload is still initializing
	downloaders []download.Downloader
	dlrByType   map[string]download.Downloader // type string → downloader (for reload)
	store       store.Store
	maxConc     int

	// persistMu serializes store writes so a snapshot taken before a state
	// change can never commit after (and clobber) that change's record.
	persistMu sync.Mutex

	errors       chan download.DownloadError
	scheduleCh   chan struct{}
	shutdownOnce sync.Once
	shutdownDone chan struct{}
	wg           sync.WaitGroup
}

func New(store store.Store, maxConcurrent int) *Manager {
	return &Manager{
		downloads:    make(map[uuid.UUID]*managedDownload),
		pendingURLs:  make(map[string]struct{}),
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
		statusChanged := false
		// Downloads that were Active when the app closed are reset to Paused.
		// Downloads that were Queued/Pending are also paused (they'll be scheduled when resumed)
		if dl.Status == download.Active || dl.Status == download.Queued || dl.Status == download.Pending {
			dl.Status = download.Paused
			statusChanged = true
		}

		m.downloads[dl.ID] = &managedDownload{
			download: dl,
			tracker:  download.NewProgressTracker(5 * time.Second),
			dirty:    statusChanged,
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
		return uuid.Nil(), ErrInvalidPriority
	}

	// Reject re-adds of a URL that is still pending, active, paused, or
	// failed (resume covers those). Beyond the UX, this prevents two live
	// torrent rows from sharing one anacrolix handle, where dropping one
	// (pause/complete) would stall the other. Completed/Cancelled records
	// don't block a fresh download of the same URL. The URL is reserved in
	// pendingURLs for the duration of Init (which can block on the network
	// for a long time) so a concurrent add of the same URL can't slip
	// through before this one lands in the downloads map.
	m.mu.Lock()

	if _, pending := m.pendingURLs[url]; pending {
		m.mu.Unlock()
		return uuid.Nil(), fmt.Errorf("%w: %s", ErrDuplicateURL, url)
	}

	for _, md := range m.downloads {
		s := md.download.Status
		if md.download.URL == url && s != download.Completed && s != download.Cancelled {
			m.mu.Unlock()
			return uuid.Nil(), fmt.Errorf("%w: %s", ErrDuplicateURL, url)
		}
	}

	m.pendingURLs[url] = struct{}{}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pendingURLs, url)
		m.mu.Unlock()
	}()

	// Find a Downloader that can handle this URL
	var dlr download.Downloader

	for _, d := range m.downloaders {
		if d.CanHandle(url) {
			dlr = d
			break
		}
	}

	if dlr == nil {
		return uuid.Nil(), ErrNoDownloader
	}

	dl, err := dlr.Init(ctx, url, priority)
	if handoff, ok := errors.AsType[*download.HandoffError](err); ok {
		dl, err = m.initHandoff(ctx, dlr, handoff, url, priority)
	}

	if err != nil {
		return uuid.Nil(), fmt.Errorf("failed to initialize download: %w", err)
	}

	dl.CreatedAt = time.Now()

	if err := m.store.Save(ctx, dl); err != nil {
		return uuid.Nil(), fmt.Errorf("failed to persist download: %w", err)
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
	md.dirty = true
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
	md.dirty = true
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
	md.dirty = true
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

	// Cancel if running. Terminal statuses are preserved so downloaders can
	// tell a completed download (final file exists) from an aborted one.
	if !md.download.Status.IsTerminal() {
		md.download.Status = download.Cancelled
	}

	if md.cancel != nil {
		md.cancel()
	}

	delete(m.downloads, id)
	dlCopy := *md.download

	// If another record references the same output (e.g. a completed row
	// plus a re-download of the same URL), deleting files — or, for
	// torrents, dropping the shared client handle — would break that other
	// record's data or freeze its live transfer. Remove only this record.
	shared := false

	for _, other := range m.downloads {
		if other.download.Type == dlCopy.Type &&
			other.download.Dir == dlCopy.Dir &&
			other.download.Filename == dlCopy.Filename {
			shared = true
			break
		}
	}
	m.mu.Unlock()

	if shared {
		slog.Info("skipping file removal: another download references the same output", "filename", dlCopy.Filename)
	} else if dlr := m.findDownloader(dlCopy.Type); dlr != nil {
		if err := dlr.Remove(&dlCopy); err != nil {
			slog.Error("failed to remove download files", "err", err)
		}
	}

	// Under persistMu so an in-flight persist batch that still holds this
	// download commits before the delete — not after it (which would
	// resurrect the record).
	m.persistMu.Lock()
	err := m.store.Delete(ctx, id)
	m.persistMu.Unlock()

	if err != nil {
		slog.Error("failed to delete download from store", "id", id, "err", err)
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

	slices.SortFunc(result, func(a, b download.DownloadInfo) int {
		return cmp.Or(
			cmp.Compare(b.Priority, a.Priority),
			bytes.Compare(a.ID[:], b.ID[:]),
		)
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
			close(m.errors)
		case <-ctx.Done():
			err = ErrShutdownTimeout
		}

		m.saveAll(context.WithoutCancel(ctx), true)

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
			m.saveAll(ctx, false)
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
		md.dirty = true
		if md.cancel != nil {
			md.cancel()
		}
	}

	running := 0

	for _, md := range m.downloads {
		if md.running {
			running++
		}
	}

	available := m.maxConc - running

	started := 0

	for _, md := range decision.toStart {
		if started >= available {
			break
		}

		// A previous Start goroutine may still be unwinding after a quick
		// pause→resume. Starting a second one would race it for the same
		// resources (torrents would share one client handle, and its
		// handleCompletion would clobber the new cancel func). Leave the
		// download Queued — handleCompletion reschedules when it exits.
		if md.running {
			continue
		}

		md.download.Status = download.Active
		md.download.StartTime = time.Now()
		md.dirty = true
		md.running = true

		runCtx, cancel := context.WithCancel(ctx)
		md.cancel = cancel

		m.wg.Add(1)

		go m.runDownload(ctx, runCtx, md)

		started++
	}

	m.mu.Unlock()
}

func (m *Manager) runDownload(managerCtx, dlCtx context.Context, md *managedDownload) {
	defer m.wg.Done()

	m.mu.RLock()
	dl := *md.download
	m.mu.RUnlock()

	dlr := m.findDownloader(dl.Type)
	if dlr == nil {
		m.mu.Lock()
		md.download.Status = download.Failed
		md.cancel = nil
		md.running = false
		md.dirty = true
		m.mu.Unlock()

		m.sendError(dl.ID, fmt.Errorf("%w: %q", ErrDownloaderNotFound, dl.Type))

		// The failure freed a scheduler slot; without this, downloads left
		// Queued by the pass that started this one would wait until the
		// next user action.
		m.requestReschedule()

		return
	}

	err := dlr.Start(dlCtx, &dl, func(downloaded, totalSize int64, state jsontext.Value) {
		md.tracker.Update(downloaded, totalSize)

		// Sync in-flight progress (and, when provided, the protocol's resume
		// state checkpoint) into the Manager-owned Download so the persist
		// loop saves it — otherwise a SIGKILL loses all progress since Start.
		// Only mark dirty on real change: a stalled download must not cost a
		// store write (and fsync) every tick.
		m.mu.Lock()
		changed := md.download.Downloaded != downloaded
		if state != nil && !bytes.Equal(md.download.State, state) {
			md.download.State = state
			changed = true
		}

		if changed {
			md.download.Downloaded = downloaded
			md.dirty = true
		}
		m.mu.Unlock()
	})

	m.handleCompletion(managerCtx, md, &dl, err)
}

func (m *Manager) handleCompletion(ctx context.Context, md *managedDownload, result *download.Download, err error) {
	m.mu.Lock()

	md.download.State = result.State
	md.download.TotalSize = result.TotalSize
	md.download.Filename = result.Filename

	if err == nil {
		md.download.Status = download.Completed
		md.download.EndTime = time.Now()
		md.download.Downloaded = md.download.TotalSize
	} else if errors.Is(err, context.Canceled) {
		snap := md.tracker.Snapshot()
		md.download.Downloaded = snap.Downloaded
	} else {
		if md.download.Status == download.Active {
			md.download.Status = download.Failed
		}

		snap := md.tracker.Snapshot()
		md.download.Downloaded = snap.Downloaded
		m.sendError(md.download.ID, err)
	}

	md.cancel = nil
	md.running = false
	md.dirty = true
	m.mu.Unlock()

	// Persist promptly via the shared batch path: it serializes against the
	// 1s tick (a stale pre-completion snapshot can't commit after this
	// record), only writes downloads still in the map (a removed download
	// isn't resurrected), and on failure leaves dirty set so the next tick
	// retries.
	m.saveAll(ctx, false)

	m.requestReschedule()
}

// saveAll persists downloads to the store in a single batched transaction.
// Only dirty downloads are written unless force is set, so idle downloads
// don't cost an fsync every tick. persistMu is held across snapshot AND
// write: without it, a snapshot taken before a status change could commit
// after the change's own write and permanently clobber it (both paths clear
// dirty, so nothing would ever re-save the newer state).
func (m *Manager) saveAll(ctx context.Context, force bool) {
	m.persistMu.Lock()
	defer m.persistMu.Unlock()

	m.mu.Lock()

	toSave := make([]*download.Download, 0, len(m.downloads))
	saved := make([]*managedDownload, 0, len(m.downloads))

	for _, md := range m.downloads {
		if !force && !md.dirty {
			continue
		}

		dlCopy := *md.download
		toSave = append(toSave, &dlCopy)
		md.dirty = false
		saved = append(saved, md)
	}

	m.mu.Unlock()

	if len(toSave) == 0 {
		return
	}

	if err := m.store.SaveBatch(ctx, toSave); err != nil {
		slog.Error("failed to persist downloads", "count", len(toSave), "err", err)

		// Re-mark so the next tick retries.
		m.mu.Lock()
		for _, md := range saved {
			md.dirty = true
		}
		m.mu.Unlock()
	}
}

// sendError sends an error on the errors channel without blocking.
func (m *Manager) sendError(id uuid.UUID, err error) {
	select {
	case m.errors <- download.DownloadError{ID: id, Error: err}:
	default:
		slog.Error("error channel full, dropping error", "id", id, "err", err)
	}
}

// findDownloader finds the registered Downloader for the given type string.
func (m *Manager) findDownloader(dlType string) download.Downloader {
	return m.dlrByType[dlType]
}

// initHandoff retries Init with the Downloader that from's probe named.
// Only one hop is followed, so Downloaders naming each other can't loop.
func (m *Manager) initHandoff(ctx context.Context, from download.Downloader, h *download.HandoffError, url string, priority int) (*download.Download, error) {
	to := m.findDownloader(h.Type)
	if to == nil || to.Type() == from.Type() {
		return nil, h
	}

	slog.Debug("handing off download", "url", url, "from", from.Type(), "to", to.Type(), "reason", h.Reason)

	return to.Init(ctx, url, priority)
}
