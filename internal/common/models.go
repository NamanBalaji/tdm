package common

import "time"

// DownloadInfo contains information about a download resource.
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

type ChunkInfo struct {
	ID                 string    `json:"id"`
	StartByte          int64     `json:"start_byte"`
	EndByte            int64     `json:"end_byte"`
	Downloaded         int64     `json:"downloaded"`
	Status             Status    `json:"status"`
	RetryCount         int       `json:"retry_count"`
	TempFilePath       string    `json:"temp_file_path"`
	SequentialDownload bool      `json:"sequential_download"`
	LastActive         time.Time `json:"last_active,omitempty"`
}

// GlobalStats contains aggregated statistics across all downloads.
type GlobalStats struct {
	ActiveDownloads    int
	QueuedDownloads    int
	CompletedDownloads int
	FailedDownloads    int
	PausedDownloads    int
	TotalDownloaded    int64
	AverageSpeed       int64
	CurrentSpeed       int64
	MaxConcurrent      int
	CurrentConcurrent  int
}

// Config contains all download configuration options.
type Config struct {
	Directory   string            `json:"directory"` // Target directory
	TempDir     string            `json:"temp_dir"`
	Connections int               `json:"connections"`       // Number of parallel connections
	Headers     map[string]string `json:"headers,omitempty"` // Custom headers

	MaxRetries int           `json:"max_retries"`           // Maximum number of retries
	RetryDelay time.Duration `json:"retry_delay,omitempty"` // Delay between retries

	ThrottleSpeed      int64 `json:"throttle_speed,omitempty"`      // Bandwidth throttle in bytes/sec
	DisableParallelism bool  `json:"disable_parallelism,omitempty"` // Force single connection

	Priority int `json:"priority"` // Priority level (higher = more important)

	Checksum          string `json:"checksum,omitempty"`           // File checksum
	ChecksumAlgorithm string `json:"checksum_algorithm,omitempty"` // Checksum algorithm

	UseExistingFile bool `json:"use_existing_file,omitempty"` // Resume from existing file
}
