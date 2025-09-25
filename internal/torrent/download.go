package torrent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrg/xdg"
	"github.com/anacrolix/torrent/metainfo"
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

// GetStatus returns the current status (thread-safe).
func (d *Download) GetStatus() status.Status {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Status
}

// SetStatus updates the status (thread-safe).
func (d *Download) SetStatus(s status.Status) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Status = s
}

// GetPriority returns the priority (thread-safe).
func (d *Download) GetPriority() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Priority
}

// SetStartTime sets the start time (thread-safe).
func (d *Download) SetStartTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.StartTime = t
}

// SetEndTime sets the end time (thread-safe).
func (d *Download) SetEndTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.EndTime = t
}

// UpdateProgress updates download/upload progress (thread-safe).
func (d *Download) UpdateProgress(downloaded, uploaded int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Downloaded = downloaded
	d.Uploaded = uploaded
}

func (d *Download) getUrl() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Url
}

func (d *Download) isMagnet() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.IsMagnet
}

// GetMetainfo returns the metainfo.
func (d *Download) GetMetainfo(ctx context.Context, client *torrentPkg.Client) (*metainfo.MetaInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return torrentPkg.GetMetainfo(d.Url)
}

// SetDownloaded atomically sets the downloaded bytes.
func (d *Download) SetDownloaded(downloaded int64) {
	atomic.StoreInt64(&d.Downloaded, downloaded)
}

// SetUploaded atomically sets the uploaded bytes.
func (d *Download) SetUploaded(uploaded int64) {
	atomic.StoreInt64(&d.Uploaded, uploaded)
}

// GetDownloaded atomically gets the downloaded bytes.
func (d *Download) GetDownloaded() int64 {
	return atomic.LoadInt64(&d.Downloaded)
}

// GetTotalSize atomically gets the total size.
func (d *Download) GetTotalSize() int64 {
	return atomic.LoadInt64(&d.TotalSize)
}

// GetName returns the download name.
func (d *Download) GetName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Name
}

// GetDir returns the download directory (thread-safe).
func (d *Download) GetDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Dir
}

// GetInfoHash returns the info hash (thread-safe).
func (d *Download) GetInfoHash() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.InfoHash
}

// Type returns the download type.
func (d *Download) Type() string {
	return "torrent"
}

// MarshalJSON custom marshaling to ensure thread-safety.
func (d *Download) MarshalJSON() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Use an alias to avoid infinite recursion
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
