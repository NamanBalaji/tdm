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
	StartByte          int64     `json:"startByte"`
	EndByte            int64     `json:"endByte"`
	Downloaded         int64     `json:"downloaded"`
	Status             Status    `json:"status"`
	RetryCount         int       `json:"retryCount"`
	TempFilePath       string    `json:"tempFilePath"`
	SequentialDownload bool      `json:"sequentialDownload"`
	LastActive         time.Time `json:"lastActive,omitempty"`
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
	Directory   string            `json:"directory"`
	TempDir     string            `json:"tempDir"`
	Connections int               `json:"connections"`
	Headers     map[string]string `json:"headers,omitempty"`

	MaxRetries int           `json:"maxRetries"`
	RetryDelay time.Duration `json:"retryDelay,omitempty"`

	ThrottleSpeed      int64 `json:"throttleSpeed,omitempty"`
	DisableParallelism bool  `json:"disableParallelism,omitempty"`

	Priority int `json:"priority"`

	Checksum          string `json:"checksum,omitempty"`
	ChecksumAlgorithm string `json:"checksumAlgorithm,omitempty"`

	UseExistingFile bool `json:"useExistingFile,omitempty"` // Resume from existing file
}
