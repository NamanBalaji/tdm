package downloader

import (
	"github.com/google/uuid"
	"time"
)

// DownloadStatus represents the current state of a download
type DownloadStatus string

const (
	StatusPending   DownloadStatus = "pending"
	StatusActive    DownloadStatus = "active"
	StatusPaused    DownloadStatus = "paused"
	StatusCompleted DownloadStatus = "completed"
	StatusFailed    DownloadStatus = "failed"
	StatusQueued    DownloadStatus = "queued"
)

// DownloadOptions contains configurable settings for a download
type DownloadOptions struct {
	URL                string
	Filename           string            // Target filename (optional, extracted from URL if empty)
	Directory          string            // Target directory for downloaded file
	Connections        int               // Number of parallel connections (0 = use default)
	Headers            map[string]string // Custom headers for the request
	MaxRetries         int               // Maximum number of retries for failed chunks/connections
	RetryDelay         time.Duration     // Delay between retries
	ThrottleSpeed      int64             // Bandwidth throttle in bytes/sec (0 = no limit)
	Checksum           string            // Optional checksum to verify download integrity
	ChecksumAlgorithm  string            // Algorithm for checksum (md5, sha256, etc.)
	Priority           int               // Priority level (higher = more important)
	UseExistingFile    bool              // Resume from existing partial download
	DisableParallelism bool              // Force single connection download
}

// DownloadStats represents real-time statistics about a download
type DownloadStats struct {
	Status           DownloadStatus
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

// Progress represents a progress update event
type Progress struct {
	DownloadID     uuid.UUID
	BytesCompleted int64
	TotalBytes     int64
	Speed          int64 // Current speed in bytes/sec
	Status         DownloadStatus
	Error          error
	Timestamp      time.Time
}
