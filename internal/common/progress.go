package common

import (
	"github.com/google/uuid"
	"time"
)

// Progress represents a progress update event for the chunk
type Progress struct {
	DownloadID     uuid.UUID
	ChunkID        uuid.UUID
	BytesCompleted int64
	TotalBytes     int64
	Speed          int64 // Current speed in bytes/sec
	Status         Status
	Error          error
	Timestamp      time.Time
}
