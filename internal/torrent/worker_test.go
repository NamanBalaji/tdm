package torrent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/status"
	torrent "github.com/NamanBalaji/tdm/internal/torrent"
)

func TestNew_ExistingDownload_ComputesInitialProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		total      int64
		downloaded int64
		status     status.Status
		wantPctMin float64
		wantPctMax float64
	}{
		{"pending with partial progress", 200, 50, status.Pending, 24.9, 25.1},
		{"completed forces 100%", 200, 0, status.Completed, 100.0, 100.0},
		{"zero total yields 0%", 0, 0, status.Pending, 0.0, 0.0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			d := &torrent.Download{
				Id:         uuid.New(),
				Name:       "file.iso",
				TotalSize:  tc.total,
				Downloaded: tc.downloaded,
				Status:     tc.status,
				Dir:        "/tmp",
				Priority:   2,
			}

			w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
			if err != nil {
				t.Fatalf("New error: %v", err)
			}

			got := w.Progress()
			if got.TotalSize != tc.total || got.Downloaded != tc.downloaded {
				t.Fatalf("size mismatch: got(total=%d,dl=%d) want(total=%d,dl=%d)",
					got.TotalSize, got.Downloaded, tc.total, tc.downloaded)
			}
			if got.Percentage < tc.wantPctMin || got.Percentage > tc.wantPctMax {
				t.Fatalf("pct=%v want [%v,%v]", got.Percentage, tc.wantPctMin, tc.wantPctMax)
			}
			if got.SpeedBPS != 0 {
				t.Fatalf("SpeedBPS=%d want 0", got.SpeedBPS)
			}
		})
	}
}

func TestWorker_Getters_And_Queue(t *testing.T) {
	t.Parallel()

	d := &torrent.Download{
		Id:       uuid.New(),
		Name:     "movie.mkv",
		Status:   status.Pending,
		Dir:      "/downloads",
		Priority: 7,
	}
	w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	t.Run("GetID", func(t *testing.T) {
		t.Parallel()
		if got := w.GetID(); got != d.Id {
			t.Fatalf("GetID=%v want %v", got, d.Id)
		}
	})
	t.Run("GetPriority", func(t *testing.T) {
		t.Parallel()
		if got := w.GetPriority(); got != d.Priority {
			t.Fatalf("GetPriority=%d want %d", got, d.Priority)
		}
	})
	t.Run("GetFilename", func(t *testing.T) {
		t.Parallel()
		if got := w.GetFilename(); got != d.Name {
			t.Fatalf("GetFilename=%q want %q", got, d.Name)
		}
	})
	t.Run("Queue->GetStatus", func(t *testing.T) {
		t.Parallel()
		w.Queue()
		if got := w.GetStatus(); got != status.Queued {
			t.Fatalf("GetStatus=%v want %v", got, status.Queued)
		}
	})
}

func TestWorker_Pause_Cancel_Transitions_And_Idempotency(t *testing.T) {
	t.Parallel()

	t.Run("Pause from Active -> Paused", func(t *testing.T) {
		t.Parallel()
		d := &torrent.Download{
			Id:       uuid.New(),
			Name:     "dataset.tar",
			Status:   status.Active, // exercise Pause path
			Dir:      t.TempDir(),
			Priority: 1,
		}
		w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if err := w.Pause(); err != nil {
			t.Fatalf("Pause error: %v", err)
		}
		if got := w.GetStatus(); got != status.Paused {
			t.Fatalf("status=%v want %v", got, status.Paused)
		}
	})

	t.Run("Cancel from Paused -> Cancelled", func(t *testing.T) {
		t.Parallel()
		d := &torrent.Download{
			Id:       uuid.New(),
			Name:     "dataset.tar",
			Status:   status.Paused,
			Dir:      t.TempDir(),
			Priority: 1,
		}
		w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if err := w.Cancel(); err != nil {
			t.Fatalf("Cancel error: %v", err)
		}
		if got := w.GetStatus(); got != status.Cancelled {
			t.Fatalf("status=%v want %v", got, status.Cancelled)
		}
	})

	t.Run("Cancel is no-op when already Completed", func(t *testing.T) {
		t.Parallel()
		d := &torrent.Download{
			Id:       uuid.New(),
			Name:     "done.bin",
			Status:   status.Completed,
			Dir:      "/tmp",
			Priority: 2,
		}
		w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if err := w.Cancel(); err != nil {
			t.Fatalf("Cancel error: %v", err)
		}
		if got := w.GetStatus(); got != status.Completed {
			t.Fatalf("status=%v want %v", got, status.Completed)
		}
	})
}

func TestWorker_Pause_NoOp_WhenNotActiveOrQueued(t *testing.T) {
	t.Parallel()

	d := &torrent.Download{
		Id:       uuid.New(),
		Name:     "report.pdf",
		Status:   status.Pending, // not Active or Queued
		Dir:      "/x",
		Priority: 3,
	}
	w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if err := w.Pause(); err != nil {
		t.Fatalf("Pause should be no-op, got %v", err)
	}
	if got := w.GetStatus(); got != status.Pending {
		t.Fatalf("status=%v want %v", got, status.Pending)
	}
}

func TestWorker_Done_Channel_InitialState(t *testing.T) {
	t.Parallel()

	d := &torrent.Download{
		Id:       uuid.New(),
		Name:     "archive.zip",
		Status:   status.Pending,
		Dir:      "/tmp",
		Priority: 5,
	}
	w, err := torrent.New(context.Background(), d, "", false, nil, nil, d.Priority)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	select {
	case <-w.Done():
		t.Fatalf("Done channel should not be signaled initially")
	default:
	}
}
