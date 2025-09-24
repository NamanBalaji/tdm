package torrent

import (
	"fmt"
	"time"
)

type Progress struct {
	TotalSize  int64
	Downloaded int64
	Percentage float64
	SpeedBPS   int64
	ETA        time.Duration
}

func (p Progress) GetTotalSize() int64    { return p.TotalSize }
func (p Progress) GetDownloaded() int64   { return p.Downloaded }
func (p Progress) GetPercentage() float64 { return p.Percentage }
func (p Progress) GetSpeedBPS() int64     { return p.SpeedBPS }
func (p Progress) GetETA() string {
	if p.ETA == 0 {
		return "unknown"
	}
	// Format ETA as Hh Mm Ss or mm ss, etc.
	hrs := int(p.ETA.Hours())
	mins := int(p.ETA.Minutes()) % 60
	secs := int(p.ETA.Seconds()) % 60

	switch {
	case hrs > 0:
		return fmt.Sprintf("%dh %dm %ds", hrs, mins, secs)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}
