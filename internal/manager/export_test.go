package manager

import (
	"context"
	"time"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
)

// SaveAllForTest exposes the persist-loop body so tests can race it against
// completions the way the 1s ticker does.
func (m *Manager) SaveAllForTest(ctx context.Context) {
	m.saveAll(ctx, false)
}

func Schedule(downloads []*download.Download, maxConcurrent int) (toStart, toPause []*download.Download) {
	managed := make(map[uuid.UUID]*managedDownload, len(downloads))
	for _, dl := range downloads {
		managed[dl.ID] = &managedDownload{
			download: dl,
			tracker:  download.NewProgressTracker(5 * time.Second),
		}
	}

	res := schedule(managed, maxConcurrent)

	for _, md := range res.toStart {
		toStart = append(toStart, md.download)
	}
	for _, md := range res.toPause {
		toPause = append(toPause, md.download)
	}

	return toStart, toPause
}
