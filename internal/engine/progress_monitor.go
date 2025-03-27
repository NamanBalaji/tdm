package engine

import (
	"context"
	"sync"

	"github.com/NamanBalaji/tdm/internal/common"
)

type ProgressMonitor struct {
	progressCh chan common.Progress
	listeners  map[string]chan<- common.Progress
	listenerMu sync.RWMutex

	done chan struct{}
}

// NewProgressMonitor creates a new progress monitor
func NewProgressMonitor(progressCh chan common.Progress) *ProgressMonitor {
	return &ProgressMonitor{
		progressCh: progressCh,
		listeners:  make(map[string]chan<- common.Progress),
		done:       make(chan struct{}),
	}
}

// Start begins listening for progress updates
func (pm *ProgressMonitor) Start(ctx context.Context) {
	go pm.monitorProgress(ctx)
}

// Stop stops the progress monitor
func (pm *ProgressMonitor) Stop() {
	close(pm.done)

	pm.listenerMu.Lock()
	defer pm.listenerMu.Unlock()

	for _, ch := range pm.listeners {
		close(ch)
	}
	pm.listeners = make(map[string]chan<- common.Progress)
}

// RegisterListener adds a new progress listener
func (pm *ProgressMonitor) RegisterListener(id string, listener chan<- common.Progress) {
	pm.listenerMu.Lock()
	defer pm.listenerMu.Unlock()

	pm.listeners[id] = listener
}

// UnregisterListener removes a progress listener
func (pm *ProgressMonitor) UnregisterListener(id string) {
	pm.listenerMu.Lock()
	defer pm.listenerMu.Unlock()

	delete(pm.listeners, id)
}

// monitorProgress is the main goroutine for forwarding progress updates
func (pm *ProgressMonitor) monitorProgress(ctx context.Context) {
	for {
		select {
		case progress := <-pm.progressCh:
			pm.broadcastProgress(&progress)
		case <-pm.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// broadcastProgress forwards progress updates to all listeners
func (pm *ProgressMonitor) broadcastProgress(progress *common.Progress) {
	pm.listenerMu.RLock()
	defer pm.listenerMu.RUnlock()

	for _, listener := range pm.listeners {
		select {
		case listener <- *progress:
		default:
		}
	}
}
