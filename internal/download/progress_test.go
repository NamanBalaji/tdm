package download_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/download"
)

func TestProgressTracker(t *testing.T) {
	t.Run("new tracker has zero snapshot", func(t *testing.T) {
		pt := download.NewProgressTracker(5 * time.Second)
		require.NotNil(t, pt)

		snap := pt.Snapshot()
		assert.Equal(t, int64(0), snap.Downloaded)
		assert.Equal(t, int64(0), snap.TotalSize)
		assert.InDelta(t, 0.0, snap.Percentage, 0.01)
		assert.Equal(t, int64(0), snap.SpeedBPS)
		assert.Equal(t, time.Duration(0), snap.ETA)
	})

	t.Run("percentage", func(t *testing.T) {
		testCases := []struct {
			name       string
			totalSize  int64
			downloaded int64
			wantPct    float64
		}{
			{"zero total size yields 0%", 0, 500, 0.0},
			{"nothing downloaded", 1000, 0, 0.0},
			{"50% downloaded", 1000, 500, 50.0},
			{"100% downloaded", 1000, 1000, 100.0},
			{"downloaded exceeds total clamped to 100%", 1000, 1500, 100.0},
			{"small fraction", 10000, 1, 0.01},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				pt := download.NewProgressTracker(5 * time.Second)
				pt.Update(tc.downloaded, tc.totalSize)

				snap := pt.Snapshot()
				assert.Equal(t, tc.totalSize, snap.TotalSize)
				assert.Equal(t, tc.downloaded, snap.Downloaded)
				assert.InDelta(t, tc.wantPct, snap.Percentage, 0.01)
			})
		}
	})

	t.Run("speed", func(t *testing.T) {
		testCases := []struct {
			name      string
			setup     func(*download.ProgressTracker)
			wantZero  bool
		}{
			{
				name: "zero with single sample",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(500, 1000)
				},
				wantZero: true,
			},
			{
				name: "positive with two samples",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(0, 10000)
					time.Sleep(50 * time.Millisecond)
					pt.Update(1000, 10000)
				},
				wantZero: false,
			},
			{
				name: "zero after reset",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(0, 1000)
					time.Sleep(10 * time.Millisecond)
					pt.Update(500, 1000)
					pt.Reset(0, 2000)
				},
				wantZero: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				pt := download.NewProgressTracker(10 * time.Second)
				tc.setup(pt)

				snap := pt.Snapshot()
				if tc.wantZero {
					assert.Equal(t, int64(0), snap.SpeedBPS)
				} else {
					assert.Greater(t, snap.SpeedBPS, int64(0))
				}
			})
		}
	})

	t.Run("ETA", func(t *testing.T) {
		testCases := []struct {
			name     string
			setup    func(*download.ProgressTracker)
			wantZero bool
		}{
			{
				name: "zero with no speed",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(500, 1000)
				},
				wantZero: true,
			},
			{
				name: "positive when in progress",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(0, 100000)
					time.Sleep(100 * time.Millisecond)
					pt.Update(1000, 100000)
				},
				wantZero: false,
			},
			{
				name: "zero when complete",
				setup: func(pt *download.ProgressTracker) {
					pt.Update(0, 1000)
					time.Sleep(10 * time.Millisecond)
					pt.Update(1000, 1000)
				},
				wantZero: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				pt := download.NewProgressTracker(10 * time.Second)
				tc.setup(pt)

				snap := pt.Snapshot()
				if tc.wantZero {
					assert.Equal(t, time.Duration(0), snap.ETA)
				} else {
					assert.Greater(t, snap.ETA, time.Duration(0))
				}
			})
		}
	})

	t.Run("reset", func(t *testing.T) {
		testCases := []struct {
			name           string
			resetDown      int64
			resetTotal     int64
			wantDownloaded int64
			wantTotalSize  int64
			wantPct        float64
		}{
			{"to zero", 0, 2000, 0, 2000, 0.0},
			{"with partial progress", 500, 2000, 500, 2000, 25.0},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				pt := download.NewProgressTracker(5 * time.Second)
				pt.Update(800, 1000)
				time.Sleep(10 * time.Millisecond)
				pt.Update(900, 1000)

				pt.Reset(tc.resetDown, tc.resetTotal)

				snap := pt.Snapshot()
				assert.Equal(t, tc.wantDownloaded, snap.Downloaded)
				assert.Equal(t, tc.wantTotalSize, snap.TotalSize)
				assert.InDelta(t, tc.wantPct, snap.Percentage, 0.01)
				assert.Equal(t, int64(0), snap.SpeedBPS)
			})
		}
	})

	t.Run("smoothing window drops old samples", func(t *testing.T) {
		pt := download.NewProgressTracker(100 * time.Millisecond)

		pt.Update(100, 10000)
		time.Sleep(150 * time.Millisecond)
		pt.Update(5000, 10000)
		time.Sleep(10 * time.Millisecond)
		pt.Update(6000, 10000)

		snap := pt.Snapshot()
		assert.Greater(t, snap.SpeedBPS, int64(0))
		assert.InDelta(t, 60.0, snap.Percentage, 0.01)
	})

	t.Run("snapshot is a value copy", func(t *testing.T) {
		pt := download.NewProgressTracker(5 * time.Second)
		pt.Update(500, 1000)
		snap1 := pt.Snapshot()

		time.Sleep(10 * time.Millisecond)
		pt.Update(700, 1000)
		snap2 := pt.Snapshot()

		assert.InDelta(t, 50.0, snap1.Percentage, 0.01)
		assert.InDelta(t, 70.0, snap2.Percentage, 0.01)
	})
}
