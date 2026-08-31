package download

import (
	"slices"
	"sync"
	"time"
)

// Progress is a point-in-time snapshot of a download's progress.
// It is a plain value type that is safe to copy, pass around, display.
type Progress struct {
	TotalSize  int64
	Downloaded int64
	Percentage float64
	SpeedBPS   int64
	ETA        time.Duration
}

// ProgressTracker computes smoothed download speed and ETA from periodic byte-count updates.
type ProgressTracker struct {
	mu              sync.Mutex
	downloaded      int64
	totalSize       int64
	samples         []sample
	smoothingWindow time.Duration
}

type sample struct {
	time  time.Time
	bytes int64
}

func NewProgressTracker(smoothingWindow time.Duration) *ProgressTracker {
	return &ProgressTracker{
		smoothingWindow: smoothingWindow,
		samples:         make([]sample, 0, 12),
	}
}

// Update records a new progress data point.
func (pt *ProgressTracker) Update(downloaded, totalSize int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now()
	pt.downloaded = downloaded
	pt.totalSize = totalSize

	pt.samples = append(pt.samples, sample{time: now, bytes: downloaded})

	cutoff := now.Add(-pt.smoothingWindow)
	pt.samples = slices.DeleteFunc(pt.samples, func(s sample) bool {
		return s.time.Before(cutoff)
	})
}

// Snapshot returns a point-in-time Progress value.
func (pt *ProgressTracker) Snapshot() Progress {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	var speedBPS int64

	if len(pt.samples) >= 2 {
		oldest := pt.samples[0]
		newest := pt.samples[len(pt.samples)-1]
		elapsed := newest.time.Sub(oldest.time).Seconds()

		if elapsed > 0 {
			speedBPS = max(int64(float64(newest.bytes-oldest.bytes)/elapsed), 0)
		}
	}

	pct := 0.0
	if pt.totalSize > 0 {
		pct = min(float64(pt.downloaded)/float64(pt.totalSize)*100, 100)
	}

	var eta time.Duration

	if speedBPS > 0 && pt.totalSize > pt.downloaded {
		remaining := pt.totalSize - pt.downloaded
		eta = time.Duration(float64(remaining)/float64(speedBPS)) * time.Second
	}

	return Progress{
		TotalSize:  pt.totalSize,
		Downloaded: pt.downloaded,
		Percentage: pct,
		SpeedBPS:   speedBPS,
		ETA:        eta,
	}
}

// Reset clears all samples.
func (pt *ProgressTracker) Reset(downloaded, totalSize int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.downloaded = downloaded
	pt.totalSize = totalSize
	pt.samples = pt.samples[:0]
}
