package manager

import (
	"time"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
)

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
