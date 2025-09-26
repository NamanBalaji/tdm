package torrent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/status"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

// Download represents a torrent download's persistent data.
type Download struct {
	mu         sync.RWMutex
	Id         uuid.UUID     `json:"id"`
	Name       string        `json:"name"`
	IsMagnet   bool          `json:"isMagnet"`
	Url        string        `json:"url"`
	TotalSize  int64         `json:"totalSize"`
	Downloaded int64         `json:"downloaded"`
	Uploaded   int64         `json:"uploaded"`
	Status     status.Status `json:"status"`
	StartTime  time.Time     `json:"startTime"`
	EndTime    time.Time     `json:"endTime,omitempty"`
	Protocol   string        `json:"protocol"`
	Dir        string        `json:"dir"`
	Priority   int           `json:"priority"`
	InfoHash   string        `json:"infoHash"`
}

// NewDownload creates a new download from a URL or magnet link.
func NewDownload(ctx context.Context, client *torrentPkg.Client, url string, isMagnet bool, priority int) (*Download, error) {
	dataDir := xdg.UserDirs.Download

	download := &Download{
		Id:       uuid.New(),
		Url:      url,
		IsMagnet: isMagnet,
		Dir:      dataDir,
		Priority: priority,
		Status:   status.Pending,
		Protocol: "torrent",
	}

	th, err := client.GetTorrentHandler(ctx, download.Url, download.IsMagnet)
	if err != nil {
		return nil, err
	}
	defer th.Drop()

	download.Name = th.Name()
	download.InfoHash = th.InfoHash().HexString()
	download.TotalSize = th.Length()

	return download, nil
}

// GetID returns the download's ID (thread-safe).
func (d *Download) GetID() uuid.UUID {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Id
}

// Type returns the download type.
func (d *Download) Type() string {
	return "torrent"
}

// MarshalJSON custom marshaling to ensure thread-safety.
func (d *Download) MarshalJSON() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	type Alias Download

	return json.Marshal((*Alias)(d))
}

// UnmarshalJSON custom unmarshaling to handle metainfo reconstruction.
func (d *Download) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion
	type Alias Download

	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(d),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	return nil
}

// getUrl returns the download URL.
func (d *Download) getUrl() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Url
}

// isMagnet returns true if the download is a magnet link.
func (d *Download) isMagnet() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.IsMagnet
}

// getStatus returns the current status (thread-safe).
func (d *Download) getStatus() status.Status {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Status
}

// setStatus updates the status (thread-safe).
func (d *Download) setStatus(s status.Status) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Status = s
}

// getPriority returns the priority (thread-safe).
func (d *Download) getPriority() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Priority
}

// setStartTime sets the start time (thread-safe).
func (d *Download) setStartTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.StartTime = t
}

// setEndTime sets the end time (thread-safe).
func (d *Download) setEndTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.EndTime = t
}

// setDownloaded atomically sets the downloaded bytes.
func (d *Download) setDownloaded(downloaded int64) {
	atomic.StoreInt64(&d.Downloaded, downloaded)
}

// getDownloaded atomically gets the downloaded bytes.
func (d *Download) getDownloaded() int64 {
	return atomic.LoadInt64(&d.Downloaded)
}

// getTotalSize atomically gets the total size.
func (d *Download) getTotalSize() int64 {
	return atomic.LoadInt64(&d.TotalSize)
}

// getName returns the download name.
func (d *Download) getName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Name
}

// getDir returns the download directory (thread-safe).
func (d *Download) getDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Dir
}
