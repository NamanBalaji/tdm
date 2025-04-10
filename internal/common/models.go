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

// Used to persist chunk data when saving downloads.
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
