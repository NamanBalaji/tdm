package downloader

import (
	"github.com/NamanBalaji/tdm/internal/common"
	"time"
)

// DownloadOptions contains configurable settings for a download
type DownloadOptions struct {
	URL                string            `json:"url"`
	Filename           string            `json:"filename"`                     // Target filename (optional, extracted from URL if empty)
	Directory          string            `json:"directory"`                    // Target directory for downloaded file
	Connections        int               `json:"connections"`                  // Number of parallel connections (0 = use default)
	Headers            map[string]string `json:"headers,omitempty"`            // Custom headers for the request
	MaxRetries         int               `json:"maxRetries"`                   // Maximum number of retries for failed chunks/connections
	RetryDelay         time.Duration     `json:"retryDelay"`                   // Delay between retries
	ThrottleSpeed      int64             `json:"throttle_speed,omitempty"`     // Bandwidth throttle in bytes/sec (0 = no limit)
	Checksum           string            `json:"checksum,omitempty"`           // Optional checksum to verify download integrity
	ChecksumAlgorithm  string            `json:"checksum_algorithm,omitempty"` // Algorithm for checksum (md5, sha256, etc.)
	Priority           int               `json:"priority"`                     // Priority level (higher = more important)
	UseExistingFile    bool              `json:"use_existing_file"`            // Resume from existing partial download
	DisableParallelism bool              `json:"disable_parallelism"`          // Force single connection download
}

// DownloadStats represents real-time statistics about a download
type DownloadStats struct {
	Status           common.Status
	TotalSize        int64
	Downloaded       int64
	Speed            int64
	AverageSpeed     int64
	TimeElapsed      time.Duration
	TimeRemaining    time.Duration
	Progress         float64
	ActiveChunks     int
	CompletedChunks  int
	TotalChunks      int
	Connections      int
	Error            string
	StartTime        time.Time
	LastUpdated      time.Time
	SpeedHistory     []int64
	BytesInLastCycle int64
}

// DownloadInfo contains information about a download resource
type DownloadInfo struct {
	URL             string
	Filename        string
	MimeType        string
	TotalSize       int64
	SupportsRanges  bool
	LastModified    time.Time
	ETag            string
	AcceptRanges    bool
	ContentEncoding string
	Server          string
	CanBeResumed    bool
}
