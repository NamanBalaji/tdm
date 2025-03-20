package engine

// GlobalStats contains aggregated statistics across all downloads
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
