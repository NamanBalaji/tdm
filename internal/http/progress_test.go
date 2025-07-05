package http_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/NamanBalaji/tdm/internal/http"
)

func TestProgress_Getters(t *testing.T) {
	p := http.Progress{
		TotalSize:  1024,
		Downloaded: 512,
		Percentage: 50.0,
		SpeedBPS:   128,
		ETA:        4 * time.Second,
	}

	assert.Equal(t, int64(1024), p.GetTotalSize(), "GetTotalSize should return the correct total size")
	assert.Equal(t, int64(512), p.GetDownloaded(), "GetDownloaded should return the correct downloaded size")
	assert.Equal(t, 50.0, p.GetPercentage(), "GetPercentage should return the correct percentage")
	assert.Equal(t, int64(128), p.GetSpeedBPS(), "GetSpeedBPS should return the correct speed")
}

func TestProgress_GetETA(t *testing.T) {
	testCases := []struct {
		name     string
		eta      time.Duration
		expected string
	}{
		{
			name:     "ETA in seconds",
			eta:      30 * time.Second,
			expected: "30s",
		},
		{
			name:     "ETA in minutes and seconds",
			eta:      2*time.Minute + 15*time.Second,
			expected: "2m 15s",
		},
		{
			name:     "ETA in hours, minutes, and seconds",
			eta:      1*time.Hour + 30*time.Minute + 5*time.Second,
			expected: "1h 30m 5s",
		},
		{
			name:     "Zero ETA",
			eta:      0,
			expected: "unknown",
		},
		{
			name:     "ETA exactly one minute",
			eta:      1 * time.Minute,
			expected: "1m 0s",
		},
		{
			name:     "ETA exactly one hour",
			eta:      1 * time.Hour,
			expected: "1h 0m 0s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := http.Progress{ETA: tc.eta}
			assert.Equal(t, tc.expected, p.GetETA())
		})
	}
}
