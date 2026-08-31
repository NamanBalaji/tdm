package manager_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/manager"
)

func TestSchedule(t *testing.T) {
	now := time.Now()

	newDL := func(status download.Status, priority int, createdOffset time.Duration) *download.Download {
		return &download.Download{
			ID:        uuid.New(),
			Status:    status,
			Priority:  priority,
			CreatedAt: now.Add(createdOffset),
			Type:      "http",
		}
	}

	t.Run("basic scheduling", func(t *testing.T) {
		testCases := []struct {
			name        string
			downloads   []*download.Download
			maxConc     int
			wantToStart int
			wantToPause int
		}{
			{
				name:        "no candidates",
				downloads:   []*download.Download{},
				maxConc:     2,
				wantToStart: 0,
				wantToPause: 0,
			},
			{
				name: "single queued download starts",
				downloads: []*download.Download{
					newDL(download.Queued, 5, 0),
				},
				maxConc:     2,
				wantToStart: 1,
				wantToPause: 0,
			},
			{
				name: "already active download not re-started",
				downloads: []*download.Download{
					newDL(download.Active, 5, 0),
				},
				maxConc:     2,
				wantToStart: 0,
				wantToPause: 0,
			},
			{
				name: "queued downloads fill up to max concurrency",
				downloads: []*download.Download{
					newDL(download.Queued, 5, 0),
					newDL(download.Queued, 5, time.Second),
					newDL(download.Queued, 5, 2*time.Second),
				},
				maxConc:     2,
				wantToStart: 2,
				wantToPause: 0,
			},
			{
				name: "terminal downloads are ignored",
				downloads: []*download.Download{
					newDL(download.Completed, 5, 0),
					newDL(download.Failed, 5, time.Second),
					newDL(download.Cancelled, 5, 2*time.Second),
					newDL(download.Queued, 5, 3*time.Second),
				},
				maxConc:     3,
				wantToStart: 1,
				wantToPause: 0,
			},
			{
				name: "paused downloads are ignored",
				downloads: []*download.Download{
					newDL(download.Paused, 5, 0),
					newDL(download.Queued, 5, time.Second),
				},
				maxConc:     2,
				wantToStart: 1,
				wantToPause: 0,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				toStart, toPause := manager.Schedule(tc.downloads, tc.maxConc)
				assert.Len(t, toStart, tc.wantToStart)
				assert.Len(t, toPause, tc.wantToPause)
			})
		}
	})

	t.Run("priority preemption", func(t *testing.T) {
		lowPri := newDL(download.Active, 1, 0)
		highPri := newDL(download.Queued, 10, time.Second)

		toStart, toPause := manager.Schedule([]*download.Download{lowPri, highPri}, 1)

		assert.Len(t, toStart, 1)
		assert.Equal(t, highPri.ID, toStart[0].ID)
		assert.Len(t, toPause, 1)
		assert.Equal(t, lowPri.ID, toPause[0].ID)
	})

	t.Run("FIFO ordering within same priority", func(t *testing.T) {
		first := newDL(download.Queued, 5, 0)
		second := newDL(download.Queued, 5, time.Second)
		third := newDL(download.Queued, 5, 2*time.Second)

		toStart, _ := manager.Schedule([]*download.Download{third, first, second}, 2)

		assert.Len(t, toStart, 2)
		ids := []uuid.UUID{toStart[0].ID, toStart[1].ID}
		assert.Contains(t, ids, first.ID)
		assert.Contains(t, ids, second.ID)
	})

	t.Run("mixed active and queued with preemption", func(t *testing.T) {
		activeLow := newDL(download.Active, 1, 0)
		activeMed := newDL(download.Active, 5, time.Second)
		queuedHigh := newDL(download.Queued, 10, 2*time.Second)

		toStart, toPause := manager.Schedule([]*download.Download{activeLow, activeMed, queuedHigh}, 2)

		assert.Len(t, toStart, 1)
		assert.Equal(t, queuedHigh.ID, toStart[0].ID)

		assert.Len(t, toPause, 1)
		assert.Equal(t, activeLow.ID, toPause[0].ID)
	})
}
