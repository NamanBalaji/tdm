package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/status"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

type Download struct {
	mu             sync.RWMutex
	Id             uuid.UUID     `json:"id"`
	URL            string        `json:"url"`
	Filename       string        `json:"filename"`
	TotalSize      int64         `json:"totalSize"`
	Downloaded     int64         `json:"downloaded"`
	Status         status.Status `json:"status"`
	StartTime      time.Time     `json:"startTime"`
	EndTime        time.Time     `json:"endTime,omitempty"`
	Chunks         []*Chunk      `json:"chunks"`
	SupportsRanges bool          `json:"supportsRanges"`
	Protocol       string        `json:"protocol"`
	Dir            string        `json:"dir"`
	TempDir        string        `json:"tempDir"`
}

func NewDownload(ctx context.Context, url string, client *httpPkg.Client, maxChunks int) (*Download, error) {
	id := uuid.New()

	download := &Download{
		Id:       id,
		URL:      url,
		Status:   status.Pending,
		Protocol: "http",
		Dir:      xdg.UserDirs.Download,
		TempDir:  filepath.Join(os.TempDir(), "tdm", id.String()),
	}

	err := download.initialize(ctx, client)
	if err != nil {
		return nil, err
	}

	download.makeChunks(maxChunks)
	err = os.MkdirAll(download.TempDir, 0o755)

	if err != nil {
		return nil, fmt.Errorf("creating temp dir %s: %w", download.TempDir, err)
	}

	return download, nil
}

func (d *Download) setDownloaded(downloaded int64) {
	atomic.StoreInt64(&d.Downloaded, downloaded)
}

func (d *Download) GetID() uuid.UUID {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Id
}

func (d *Download) getURL() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.URL
}

func (d *Download) getTempDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.TempDir
}

func (d *Download) getDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Dir
}

func (d *Download) getStatus() status.Status {
	return atomic.LoadInt32(&d.Status)
}

func (d *Download) setStatus(status status.Status) {
	atomic.StoreInt32(&d.Status, status)
}

func (d *Download) getTotalSize() int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.TotalSize
}

func (d *Download) getSupportsRanges() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.SupportsRanges
}

func (d *Download) getDownloadableChunks() []*Chunk {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var downloadableChunks []*Chunk

	for _, chunk := range d.Chunks {
		if chunk.getStatus() != status.Completed {
			downloadableChunks = append(downloadableChunks, chunk)
		}
	}

	return downloadableChunks
}

func (d *Download) getChunks() []*Chunk {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.Chunks
}

func (d *Download) setStartTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.StartTime = t
}

func (d *Download) setEndTime(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.EndTime = t
}

func (d *Download) initialize(ctx context.Context, client *httpPkg.Client) error {
	var err error
	err = d.initializeWithHEAD(ctx, client)
	if err != nil {
		logger.Warnf("HEAD request failed, falling back. Error: %v", err)

		if httpPkg.IsFallbackError(err) {
			err = d.initializeWithRangeGET(ctx, client)
			if err != nil {
				logger.Warnf("Range GET request failed, falling back. Error: %v", err)

				if httpPkg.IsFallbackError(err) {
					err = d.initializeWithRegularGET(ctx, client)
					if err != nil {
						return err
					}
				} else {
					return err
				}
			}
		} else {
			return err
		}
	}

	return nil
}

func (d *Download) initializeWithHEAD(ctx context.Context, client *httpPkg.Client) error {
	logger.Debugf("Initializing with HEAD request: %s", d.URL)

	resp, err := client.Head(ctx, d.URL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	supportsRanges := resp.Header.Get("Accept-Ranges") == "bytes"
	logger.Debugf("HEAD request successful, content-length=%d, supports-ranges=%v", resp.ContentLength, supportsRanges)
	d.populate(resp, supportsRanges, resp.ContentLength)

	return nil
}

func (d *Download) initializeWithRangeGET(ctx context.Context, client *httpPkg.Client) error {
	logger.Debugf("Initializing with Range GET request: %s", d.URL)

	resp, err := client.Range(ctx, d.URL, 0, 0, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	contentRange := resp.Header.Get("Content-Range")

	var totalSize int64 = 0

	if contentRange != "" {
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			size, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				logger.Warnf("Failed to parse size from Content-Range header: %s", contentRange)
				return httpPkg.ErrInvalidContentRange
			}

			totalSize = size
		}
	}

	logger.Debugf("Range GET request successful, supports-ranges=true, content-length=%d", totalSize)
	d.populate(resp, true, totalSize)

	return nil
}

func (d *Download) initializeWithRegularGET(ctx context.Context, client *httpPkg.Client) error {
	logger.Debugf("Initializing with regular GET request: %s", d.URL)

	resp, err := client.Get(ctx, d.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	logger.Debugf("Regular GET request successful, content-length=%d", resp.ContentLength)
	d.populate(resp, false, resp.ContentLength)

	return nil
}

func (d *Download) populate(resp *http.Response, canRange bool, totalSize int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	logger.Debugf("Generating download info for %s", resp.Request.URL)

	d.TotalSize = totalSize
	d.Filename = httpPkg.GetFilename(resp)
	d.SupportsRanges = canRange
}

func (d *Download) makeChunks(numChunks int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.Chunks = nil

	if !d.SupportsRanges || d.TotalSize <= 0 {
		c := newChunk(0, d.TotalSize-1, d.TempDir)

		logger.Debugf("Created single chunk because ranges are not supported or size is unknown.")

		d.Chunks = append(d.Chunks, c)

		return
	}

	chunkSize := d.TotalSize / int64(numChunks)
	if chunkSize <= 0 {
		chunkSize = d.TotalSize
	}

	var startByte int64
	for i := range numChunks {
		endByte := startByte + chunkSize - 1
		if i == numChunks-1 || endByte >= d.TotalSize-1 {
			endByte = d.TotalSize - 1
		}

		c := newChunk(startByte, endByte, d.TempDir)
		d.Chunks = append(d.Chunks, c)
		logger.Debugf("Created chunk %d with Id: %s, range: %d-%d", i, c.ID, startByte, endByte)

		startByte = endByte + 1

		if startByte >= d.TotalSize {
			break
		}
	}
}

func (d *Download) MarshalJSON() ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	type Alias Download

	return json.Marshal(&struct {
		Status int32 `json:"status"`
		*Alias
	}{
		Status: d.getStatus(),
		Alias:  (*Alias)(d),
	})
}

func (d *Download) Type() string {
	return "http"
}
