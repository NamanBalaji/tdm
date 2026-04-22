package manager

import (
	"sort"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/download"
)

// scheduleResult contains the list of downloads to start
// and to pause, based on priority and the max concurrency limit.
type scheduleResult struct {
	toStart []*managedDownload
	toPause []*managedDownload
}

// schedule decides which downloads should be active given the concurrency limit.
func schedule(downloads map[uuid.UUID]*managedDownload, maxConcurrent int) scheduleResult {
	var candidates []*managedDownload

	for _, md := range downloads {
		s := md.download.Status
		if s == download.Active || s == download.Queued || s == download.Pending {
			candidates = append(candidates, md)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].download.Priority != candidates[j].download.Priority {
			return candidates[i].download.Priority > candidates[j].download.Priority
		}

		return candidates[i].download.CreatedAt.Before(candidates[j].download.CreatedAt)
	})

	limit := min(maxConcurrent, len(candidates))

	shouldBeActive := make(map[uuid.UUID]bool, limit)
	for i := range limit {
		shouldBeActive[candidates[i].download.ID] = true
	}

	var result scheduleResult

	for i := range limit {
		md := candidates[i]
		if md.download.Status != download.Active {
			result.toStart = append(result.toStart, md)
		}
	}

	for _, md := range downloads {
		if md.download.Status == download.Active && !shouldBeActive[md.download.ID] {
			result.toPause = append(result.toPause, md)
		}
	}

	return result
}
