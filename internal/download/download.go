// Package download defines the core data model for downloads.
package download

import (
	"encoding/json/jsontext"
	"time"
	"uuid"
)

// Download is the central, protocol-agnostic data model for a download.
type Download struct {
	ID         uuid.UUID `json:"id"`
	URL        string    `json:"url"`
	Filename   string    `json:"filename"`
	Dir        string    `json:"dir"`
	Status     Status    `json:"status"`
	Priority   int       `json:"priority"`
	Type       string    `json:"type"` // "http", "torrent"
	TotalSize  int64     `json:"totalSize"`
	Downloaded int64     `json:"downloaded"`
	StartTime  time.Time `json:"startTime"`
	EndTime    time.Time `json:"endTime"`
	CreatedAt  time.Time `json:"createdAt"`

	// State holds protocol-specific state (chunks for HTTP, infohash for torrent, etc.) as opaque JSON.
	State jsontext.Value `json:"backendState,omitempty"`
}

// DownloadInfo is the read-only view returned to the TUI.
type DownloadInfo struct {
	ID       uuid.UUID
	Filename string
	Status   Status
	Priority int
	Progress Progress
}

type DownloadError struct {
	ID    uuid.UUID
	Error error
}
