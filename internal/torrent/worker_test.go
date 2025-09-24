package torrent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/torrent"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

func newDownloadForTest(name string, sz, downloaded int64, st status.Status, dir string, prio int) *torrent.Download {
	return &torrent.Download{
		Id:         uuid.New(),
		Name:       name,
		TotalSize:  sz,
		Downloaded: downloaded,
		Status:     st,
		Dir:        dir,
		Priority:   prio,
		Protocol:   "torrent",
	}
}

func TestNew_WithExistingDownload_ComputesInitialProgress(t *testing.T) {
	tmp := t.TempDir()

	cases := []struct {
		name       string
		total      int64
		downloaded int64
		status     status.Status
		wantPct    float64
	}{
		{"zero total", 0, 0, status.Pending, 0},
		{"some progress", 1000, 400, status.Pending, 40},
		{"completed forces 100", 1000, 10, status.Completed, 100},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := newDownloadForTest("file.iso", tc.total, tc.downloaded, tc.status, tmp, 5)
			w, err := torrent.New(context.Background(), d, "", false, nil, nil, 5)
			if err != nil {
				t.Fatalf("New unexpected error: %v", err)
			}
			gotProg := w.Progress()
			if gotProg.GetTotalSize() != tc.total {
				t.Fatalf("total=%d want=%d", gotProg.GetTotalSize(), tc.total)
			}
			if gotProg.GetDownloaded() != tc.downloaded {
				t.Fatalf("downloaded=%d want=%d", gotProg.GetDownloaded(), tc.downloaded)
			}
			if tc.wantPct == 100 && gotProg.GetPercentage() != 100 {
				t.Fatalf("pct=%v want=100", gotProg.GetPercentage())
			}
			if tc.wantPct != 100 && int(gotProg.GetPercentage()) != int(tc.wantPct) {
				t.Fatalf("pct=%v want≈%v", gotProg.GetPercentage(), tc.wantPct)
			}
		})
	}
}

func TestWorker_Getters_And_Queue(t *testing.T) {
	tmp := t.TempDir()
	d := newDownloadForTest("movie.mkv", 2048, 256, status.Pending, tmp, 7)
	w, err := torrent.New(context.Background(), d, "", false, nil, nil, 7)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	if w.GetFilename() != "movie.mkv" {
		t.Fatalf("GetFilename=%q want %q", w.GetFilename(), "movie.mkv")
	}
	if w.GetPriority() != 7 {
		t.Fatalf("GetPriority=%d want 7", w.GetPriority())
	}
	if w.GetID() != d.Id {
		t.Fatalf("GetID mismatch")
	}
	if w.GetStatus() != status.Pending {
		t.Fatalf("GetStatus=%v want Pending", w.GetStatus())
	}

	w.Queue()
	if w.GetStatus() != status.Queued {
		t.Fatalf("after Queue status=%v want Queued", w.GetStatus())
	}
}

func TestWorker_Start_EarlyReturns(t *testing.T) {
	tmp := t.TempDir()

	t.Run("completed no-op", func(t *testing.T) {
		d := newDownloadForTest("done.bin", 100, 100, status.Completed, tmp, 1)
		w, err := torrent.New(context.Background(), d, "", false, &torrentPkg.Client{}, nil, 1)
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if err := w.Start(context.Background()); err != nil {
			t.Fatalf("Start unexpected error: %v", err)
		}
	})

	t.Run("no client returns ErrNoClient", func(t *testing.T) {
		d := newDownloadForTest("pending.bin", 1000, 0, status.Pending, tmp, 1)
		w, err := torrent.New(context.Background(), d, "", false, &torrentPkg.Client{}, nil, 1)
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		err = w.Start(context.Background())
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestWorker_Pause_Cancel_NoRepo(t *testing.T) {
	tmp := t.TempDir()

	t.Run("pause no-op when not active or queued", func(t *testing.T) {
		d := newDownloadForTest("file", 10, 0, status.Pending, tmp, 1)
		w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)
		if err := w.Pause(); err != nil {
			t.Fatalf("Pause unexpected error: %v", err)
		}
		if w.GetStatus() != status.Pending {
			t.Fatalf("status changed, got %v", w.GetStatus())
		}
	})

	t.Run("pause transitions from active to paused", func(t *testing.T) {
		d := newDownloadForTest("file", 10, 0, status.Active, tmp, 1)
		w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)
		if err := w.Pause(); err != nil {
			t.Fatalf("Pause error: %v", err)
		}
		if w.GetStatus() != status.Paused {
			t.Fatalf("status=%v want Paused", w.GetStatus())
		}
	})

	t.Run("cancel sets cancelled", func(t *testing.T) {
		d := newDownloadForTest("file", 10, 0, status.Pending, tmp, 1)
		w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)
		if err := w.Cancel(); err != nil {
			t.Fatalf("Cancel error: %v", err)
		}
		if w.GetStatus() != status.Cancelled {
			t.Fatalf("status=%v want Cancelled", w.GetStatus())
		}
	})
}

func TestWorker_Done_Channel_And_Progress_Interface(t *testing.T) {
	tmp := t.TempDir()
	d := newDownloadForTest("x", 500, 100, status.Pending, tmp, 2)
	w, err := torrent.New(context.Background(), d, "", false, nil, nil, 2)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	select {
	case <-w.Done():
		t.Fatalf("unexpected value on Done channel")
	default:
	}

	var p progress.Progress = w.Progress()
	if p.GetTotalSize() != 500 || p.GetDownloaded() != 100 {
		t.Fatalf("progress snapshot mismatch: %#v", p)
	}
	_ = p.GetETA()
}

func TestWorker_Stop_FastPath_WhenAlreadyTerminal(t *testing.T) {
	tmp := t.TempDir()
	d := newDownloadForTest("y", 1, 1, status.Failed, tmp, 1)
	w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)

	if err := w.Cancel(); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}
	if w.GetStatus() != status.Failed {
		t.Fatalf("status=%v want Failed", w.GetStatus())
	}
}

func TestWorker_QueuePause_Table(t *testing.T) {
	tmp := t.TempDir()
	cases := []struct {
		name  string
		start status.Status
		want  status.Status
	}{
		{"queued to paused", status.Queued, status.Paused},
		{"active to paused", status.Active, status.Paused},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := newDownloadForTest("z", 0, 0, tc.start, tmp, 1)
			w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)
			if err := w.Pause(); err != nil {
				t.Fatalf("Pause error: %v", err)
			}
			if w.GetStatus() != tc.want {
				t.Fatalf("status=%v want %v", w.GetStatus(), tc.want)
			}
		})
	}
}

func TestWorker_Progress_SnapshotStable(t *testing.T) {
	tmp := t.TempDir()
	d := newDownloadForTest("snap", 100, 10, status.Pending, tmp, 1)
	w, _ := torrent.New(context.Background(), d, "", false, nil, nil, 1)

	p1 := w.Progress()
	d.SetDownloaded(90)
	p2 := w.Progress()
	if p1.GetDownloaded() != p2.GetDownloaded() {
		t.Fatalf("progress snapshot should be stable between calls without tracker running")
	}
	_ = time.Second
}
