package torrent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

var (
	ErrAlreadyStarted = errors.New("download already started")
	ErrNoClient       = errors.New("torrent client not available")
	ErrNoMetainfo     = errors.New("no metainfo available")
)

// Worker implements the worker.Worker interface for torrents.
type Worker struct {
	download *Download
	repo     *repository.BboltRepository

	torrentMu     sync.RWMutex
	torrentHandle *torrent.Torrent
	client        *torrentPkg.Client

	// State management
	started  atomic.Bool
	finished atomic.Bool
	stopping atomic.Bool

	done     chan error
	cancel   context.CancelFunc
	cancelMu sync.Mutex

	// Progress tracking
	progressMu   sync.RWMutex
	lastProgress Progress

	wg sync.WaitGroup
}

// New creates a new torrent worker.
func New(ctx context.Context, downloadData *Download, url string, isMagnet bool, client *torrentPkg.Client, repo *repository.BboltRepository, priority int) (*Worker, error) {
	var (
		download *Download
		err      error
	)

	if downloadData == nil {
		download, err = NewDownload(ctx, client, url, isMagnet, priority)
		if err != nil {
			return nil, err
		}

		if repo != nil {
			if err := repo.Save(download); err != nil {
				return nil, fmt.Errorf("failed to save download: %w", err)
			}
		}
	} else {
		download = downloadData
	}

	downloaded, totalSize := download.GetDownloaded(), download.GetTotalSize()

	var pct float64
	if totalSize > 0 {
		pct = float64(downloaded) / float64(totalSize) * 100
	}

	if download.GetStatus() == status.Completed {
		pct = 100.0
	}

	prog := Progress{
		TotalSize:  totalSize,
		Downloaded: downloaded,
		Percentage: pct,
		SpeedBPS:   0,
		ETA:        0,
	}

	return &Worker{
		download:     download,
		repo:         repo,
		client:       client,
		done:         make(chan error, 1),
		lastProgress: prog,
	}, nil
}

// GetPriority returns the download priority.
func (w *Worker) GetPriority() int {
	return w.download.GetPriority()
}

// GetID returns the download ID.
func (w *Worker) GetID() uuid.UUID {
	return w.download.GetID()
}

// GetStatus returns the current status.
func (w *Worker) GetStatus() status.Status {
	return w.download.GetStatus()
}

// GetFilename returns the torrent name.
func (w *Worker) GetFilename() string {
	return w.download.GetName()
}

// Queue marks the download as queued.
func (w *Worker) Queue() {
	w.download.SetStatus(status.Queued)
}

// Start begins downloading the torrent.
func (w *Worker) Start(ctx context.Context) error {
	if !w.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	currentStatus := w.download.GetStatus()
	if currentStatus == status.Completed || currentStatus == status.Cancelled {
		w.started.Store(false)
		return nil
	}

	client := w.client.GetClient()
	if client == nil {
		w.started.Store(false)
		return ErrNoClient
	}

	mi, err := w.download.GetMetainfo(ctx, w.client)
	if err != nil {
		w.started.Store(false)
		return fmt.Errorf("failed to get metainfo: %w", err)
	}

	if mi == nil {
		w.started.Store(false)
		return ErrNoMetainfo
	}

	t, err := client.AddTorrent(mi)
	if err != nil {
		w.started.Store(false)
		return fmt.Errorf("failed to add torrent: %w", err)
	}

	w.torrentMu.Lock()
	w.torrentHandle = t
	w.torrentMu.Unlock()

	w.stopping.Store(false)

	select {
	case <-ctx.Done():
		w.dropTorrent()
		w.started.Store(false)

		return ctx.Err()
	case <-t.GotInfo():
	}

	if err := t.VerifyDataContext(ctx); err != nil {
		return fmt.Errorf("failed to verify torrent: %w", err)
	}

	t.DownloadAll()

	// Update status
	w.download.SetStatus(status.Active)
	w.download.SetStartTime(time.Now())

	runCtx, cancel := context.WithCancel(ctx)
	w.setCancel(cancel)

	w.wg.Add(3)

	go w.saveState(runCtx)
	go w.trackProgress(runCtx)
	go w.waitCompletion(runCtx)

	return nil
}

// Pause suspends the download.
func (w *Worker) Pause() error {
	currentStatus := w.download.GetStatus()
	if currentStatus != status.Active && currentStatus != status.Queued {
		return nil
	}

	return w.stop(status.Paused, false)
}

// Cancel stops the download.
func (w *Worker) Cancel() error {
	return w.stop(status.Cancelled, false)
}

// Remove cancels and deletes the download.
func (w *Worker) Remove() error {
	return w.stop(status.Cancelled, true)
}

// Done returns a channel that signals completion.
func (w *Worker) Done() <-chan error {
	return w.done
}

// Progress returns current progress.
func (w *Worker) Progress() progress.Progress {
	w.progressMu.RLock()
	defer w.progressMu.RUnlock()

	return w.lastProgress
}

// setCancel stores the cancel function.
func (w *Worker) setCancel(cf context.CancelFunc) {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()

	w.cancel = cf
}

// getCancel retrieves the cancel function.
func (w *Worker) getCancel() context.CancelFunc {
	w.cancelMu.Lock()
	defer w.cancelMu.Unlock()

	return w.cancel
}

// snapshotHandle safely retrieves the torrent handle.
func (w *Worker) snapshotHandle() *torrent.Torrent {
	w.torrentMu.RLock()
	defer w.torrentMu.RUnlock()

	if w.stopping.Load() {
		return nil
	}

	return w.torrentHandle
}

// dropTorrent removes the torrent from the client.
func (w *Worker) dropTorrent() {
	w.stopping.Store(true)

	w.torrentMu.Lock()
	defer w.torrentMu.Unlock()

	if w.torrentHandle != nil {
		w.torrentHandle.Drop()
		w.torrentHandle = nil
	}
}

// saveState periodically saves the download state.
func (w *Worker) saveState(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if w.repo != nil && !w.stopping.Load() {
				_ = w.repo.Save(w.download)
			}
		}
	}
}

// trackProgress periodically updates download progress.
func (w *Worker) trackProgress(ctx context.Context) {
	defer w.wg.Done()

	const (
		tickInterval    = 500 * time.Millisecond
		smoothingWindow = 5 * time.Second
	)

	type speedSample struct {
		time  time.Time
		bytes int64
	}

	samples := make([]speedSample, 0, 10)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			t := w.snapshotHandle()
			if t == nil {
				continue
			}

			var downloaded int64
			if t.Info() != nil {
				downloaded = t.BytesCompleted()
			}

			w.download.SetDownloaded(downloaded)

			samples = append(samples, speedSample{
				time:  now,
				bytes: downloaded,
			})

			cutoff := now.Add(-smoothingWindow)

			i := 0
			for i < len(samples) && samples[i].time.Before(cutoff) {
				i++
			}

			samples = samples[i:]

			var avgSpeed float64

			if len(samples) >= 2 {
				timeDelta := samples[len(samples)-1].time.Sub(samples[0].time).Seconds()

				bytesDelta := samples[len(samples)-1].bytes - samples[0].bytes
				if timeDelta > 0 {
					avgSpeed = float64(bytesDelta) / timeDelta
				}
			}

			total := w.download.GetTotalSize()

			var pct float64
			if total > 0 {
				pct = (float64(downloaded) / float64(total)) * 100.0
				if pct > 100.0 {
					pct = 100.0
				}
			}

			var eta time.Duration
			if avgSpeed > 0 && total > downloaded {
				eta = time.Duration(float64(total-downloaded)/avgSpeed) * time.Second
			}

			w.progressMu.Lock()
			w.lastProgress = Progress{
				TotalSize:  total,
				Downloaded: downloaded,
				Percentage: pct,
				SpeedBPS:   int64(avgSpeed),
				ETA:        eta,
			}
			w.progressMu.Unlock()
		}
	}
}

// waitCompletion monitors for download completion.
func (w *Worker) waitCompletion(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t := w.snapshotHandle()
			if t == nil {
				return
			}

			if t.Complete().Bool() && t.BytesCompleted() >= t.Length() {
				if !w.finished.Swap(true) {
					w.download.SetStatus(status.Completed)
					w.download.SetEndTime(time.Now())

					if w.repo != nil {
						_ = w.repo.Save(w.download)
					}

					select {
					case w.done <- nil:
					default:
					}
				}

				return
			}
		}
	}
}

// stop halts the download and optionally removes files.
func (w *Worker) stop(targetStatus status.Status, remove bool) error {
	current := w.download.GetStatus()
	if current == status.Completed || current == status.Failed || current == status.Cancelled {
		if remove {
			return w.cleanup()
		}

		return nil
	}

	if cancelFunc := w.getCancel(); cancelFunc != nil {
		cancelFunc()
	}

	w.dropTorrent()

	w.download.SetStatus(targetStatus)

	done := make(chan struct{})

	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	if w.repo != nil && !remove {
		_ = w.repo.Save(w.download)
	}

	w.started.Store(false)

	if remove {
		return w.cleanup()
	}

	return nil
}

// cleanup removes downloaded files and deletes the record from the repository.
func (w *Worker) cleanup() error {
	downloadPath := filepath.Join(w.download.GetDir(), w.download.GetName())
	_ = os.RemoveAll(downloadPath)

	return w.repo.Delete(w.download.GetID().String())
}
