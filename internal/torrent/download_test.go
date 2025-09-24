package torrent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/torrent"
)

func TestDownload_GettersSetters(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(2 * time.Minute)

	id := uuid.New()
	d := &torrent.Download{
		Id:         id,
		Name:       "test.torrent",
		IsMagnet:   false,
		Url:        "http://example.com/file.torrent",
		TotalSize:  1024,
		Downloaded: 128,
		Uploaded:   64,
		Status:     status.Pending,
		StartTime:  now,
		EndTime:    time.Time{},
		Protocol:   "torrent",
		Dir:        "/tmp",
		Priority:   3,
		InfoHash:   "abcdef1234",
	}

	t.Run("GetID", func(t *testing.T) {
		if got := d.GetID(); got != id {
			t.Fatalf("GetID = %v, want %v", got, id)
		}
	})

	t.Run("Status getters and setters", func(t *testing.T) {
		if got := d.GetStatus(); got != status.Pending {
			t.Fatalf("GetStatus = %v, want %v", got, status.Pending)
		}
		d.SetStatus(status.Active)
		if got := d.GetStatus(); got != status.Active {
			t.Fatalf("GetStatus(after SetStatus) = %v, want %v", got, status.Active)
		}
	})

	t.Run("Priority getter", func(t *testing.T) {
		if got := d.GetPriority(); got != 3 {
			t.Fatalf("GetPriority = %v, want %v", got, 3)
		}
	})

	t.Run("Start and End time setters", func(t *testing.T) {
		d.SetStartTime(now)
		d.SetEndTime(later)
		if d.StartTime != now {
			t.Fatalf("StartTime = %v, want %v", d.StartTime, now)
		}
		if d.EndTime != later {
			t.Fatalf("EndTime = %v, want %v", d.EndTime, later)
		}
	})

	t.Run("UpdateProgress", func(t *testing.T) {
		d.UpdateProgress(900, 700)
		if d.Downloaded != 900 || d.Uploaded != 700 {
			t.Fatalf("UpdateProgress did not set fields, got downloaded=%d uploaded=%d", d.Downloaded, d.Uploaded)
		}
	})

	t.Run("Atomic setters/getters", func(t *testing.T) {
		d.SetDownloaded(777)
		d.SetUploaded(333)
		if got := d.GetDownloaded(); got != 777 {
			t.Fatalf("GetDownloaded = %d, want %d", got, 777)
		}
		if got := d.GetTotalSize(); got != 1024 {
			t.Fatalf("GetTotalSize = %d, want %d", got, 1024)
		}
	})

	t.Run("Read-only getters", func(t *testing.T) {
		if got := d.GetName(); got != "test.torrent" {
			t.Fatalf("GetName = %q, want %q", got, "test.torrent")
		}
		if got := d.GetDir(); got != "/tmp" {
			t.Fatalf("GetDir = %q, want %q", got, "/tmp")
		}
		if got := d.GetInfoHash(); got != "abcdef1234" {
			t.Fatalf("GetInfoHash = %q, want %q", got, "abcdef1234")
		}
	})

	t.Run("Type", func(t *testing.T) {
		if got := d.Type(); got != "torrent" {
			t.Fatalf("Type = %q, want %q", got, "torrent")
		}
	})
}

func TestDownload_JSON_MarshalUnmarshal(t *testing.T) {
	orig := &torrent.Download{
		Id:         uuid.New(),
		Name:       "movie.iso",
		IsMagnet:   true,
		Url:        "magnet:?xt=urn:btih:abcdef",
		TotalSize:  2048,
		Downloaded: 1024,
		Uploaded:   512,
		Status:     status.Active,
		StartTime:  time.Now().UTC().Truncate(time.Second),
		EndTime:    time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second),
		Protocol:   "torrent",
		Dir:        "/home/user/Downloads",
		Priority:   9,
		InfoHash:   "deadbeefcafebabe",
	}

	data, err := orig.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	var got torrent.Download
	if err := got.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}

	// Spot-check key fields preserved
	if got.Id != orig.Id || got.Name != orig.Name || got.InfoHash != orig.InfoHash {
		t.Fatalf("Unmarshal mismatch: got %+v, want %+v", got, *orig)
	}
	if got.Status != orig.Status || got.Priority != orig.Priority {
		t.Fatalf("Unmarshal mismatch on status/priority: got %+v, want %+v", got.Status, orig.Status)
	}
}

func TestDownload_GetMetainfo_NonMagnet_Error(t *testing.T) {
	d := &torrent.Download{
		IsMagnet: false,
		Url:      "::::invalid::::",
	}
	_, err := d.GetMetainfo(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for invalid URL in GetMetainfo, got nil")
	}
}

func TestNewDownload_ErrorPath(t *testing.T) {
	_, err := torrent.NewDownload(context.Background(), nil, "::::invalid::::", false, 5)
	if err == nil {
		t.Fatalf("expected error from NewDownload with invalid URL, got nil")
	}
}

func TestDownload_Minors(t *testing.T) {
	cases := []struct {
		name  string
		setup func() *torrent.Download
		check func(t *testing.T, d *torrent.Download)
	}{
		{
			name: "SetStatus transitions",
			setup: func() *torrent.Download {
				return &torrent.Download{Status: status.Pending}
			},
			check: func(t *testing.T, d *torrent.Download) {
				d.SetStatus(status.Paused)
				if d.GetStatus() != status.Paused {
					t.Fatalf("status not updated")
				}
			},
		},
		{
			name: "UpdateProgress overwrites",
			setup: func() *torrent.Download {
				return &torrent.Download{Downloaded: 1, Uploaded: 1}
			},
			check: func(t *testing.T, d *torrent.Download) {
				d.UpdateProgress(10, 20)
				if d.Downloaded != 10 || d.Uploaded != 20 {
					t.Fatalf("progress not updated")
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := tc.setup()
			tc.check(t, d)
		})
	}
}

func TestMarshalJSON_UsesAlias(t *testing.T) {
	d := &torrent.Download{Name: "alias-check"}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var out torrent.Download
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if out.Name != "alias-check" {
		t.Fatalf("unexpected name: %q", out.Name)
	}
}
