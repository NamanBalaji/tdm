package progress

import (
	"fmt"
	"time"
)

// Progress holds the download progress information.
type Progress struct {
	// Total size of the download in bytes.
	TotalSize int64
	// Bytes downloaded so far.
	Downloaded int64
	// Progress percentage.
	Percentage float64
	// Current download speed in bytes per second.
	SpeedBPS int64
	// Estimated time remaining for the download to complete.
	ETA time.Duration
}

// GetTotalSize returns the total size of the download.
func (p Progress) GetTotalSize() int64 {
	return p.TotalSize
}

// GetDownloaded returns the number of bytes downloaded.
func (p Progress) GetDownloaded() int64 {
	return p.Downloaded
}

// GetPercentage returns the download percentage.
func (p Progress) GetPercentage() float64 {
	return p.Percentage
}

// GetSpeedBPS returns the download speed in bytes per second.
func (p Progress) GetSpeedBPS() int64 {
	return p.SpeedBPS
}

// GetETA returns a human-readable ETA string.
func (p Progress) GetETA() string {
	if p.ETA == 0 {
		return "unknown"
	}

	// Format duration in a human-readable way
	hours := int(p.ETA.Hours())
	minutes := int(p.ETA.Minutes()) % 60
	seconds := int(p.ETA.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}

	return fmt.Sprintf("%ds", seconds)
}
