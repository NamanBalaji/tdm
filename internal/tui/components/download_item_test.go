package components_test

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/tui/components"
)

func TestDownloadItem(t *testing.T) {
	longFilename := "this-is-a-very-long-filename-that-will-definitely-be-truncated.zip"
	shortFilename := "file.txt"

	testCases := []struct {
		name           string
		info           download.DownloadInfo
		width          int
		selected       bool
		expectedChecks []string
	}{
		{
			name: "Active Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: shortFilename,
				Status:   download.Active,
				Progress: download.Progress{
					TotalSize:  1000,
					Downloaded: 500,
					Percentage: 50.0,
					SpeedBPS:   100,
					ETA:        5 * time.Second,
				},
			},
			width:    80,
			selected: false,
			expectedChecks: []string{
				shortFilename,
				"active",
				"50.0%",
				"500 B / 1000 B",
				"100 B/s",
				"ETA: 5s",
			},
		},
		{
			name: "Selected Paused Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: shortFilename,
				Status:   download.Paused,
				Progress: download.Progress{TotalSize: 2048, Downloaded: 1024, Percentage: 50.0},
			},
			width:    80,
			selected: true,
			expectedChecks: []string{
				"paused",
				"1.0 KiB / 2.0 KiB",
				"--/s",
				"ETA: --",
			},
		},
		{
			name: "completed Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: "completed.iso",
				Status:   download.Completed,
				Progress: download.Progress{TotalSize: 5000000, Downloaded: 5000000, Percentage: 100.0},
			},
			width:    100,
			selected: false,
			expectedChecks: []string{
				"completed.iso",
				"completed",
				"100.0%",
				"4.8 MiB / 4.8 MiB",
				"ETA: Done",
			},
		},
		{
			name: "Failed Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: "failed_download",
				Status:   download.Failed,
				Progress: download.Progress{TotalSize: 1000, Downloaded: 100, Percentage: 10.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"failed_download", "failed", "10.0%"},
		},
		{
			name: "Cancelled Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: "cancelled.tar.gz",
				Status:   download.Cancelled,
				Progress: download.Progress{TotalSize: 1000, Downloaded: 200, Percentage: 20.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"cancelled.tar.gz", "cancelled", "20.0%"},
		},
		{
			name: "Queued Download",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: "queued_file",
				Status:   download.Queued,
				Progress: download.Progress{TotalSize: 5000, Downloaded: 0, Percentage: 0.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"queued_file", "queued", "0.0%", "0 B / 4.9 KiB"},
		},
		{
			name: "Long Filename Truncation",
			info: download.DownloadInfo{
				ID:       uuid.New(),
				Filename: longFilename,
				Status:   download.Active,
				Progress: download.Progress{TotalSize: 1000, Downloaded: 10, Percentage: 1.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"..."},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := components.DownloadItem(tc.info, tc.width, tc.selected)
			for _, check := range tc.expectedChecks {
				if !strings.Contains(output, check) {
					t.Errorf("expected output to contain %q, but it did not.\nOutput:\n%s", check, output)
				}
			}
		})
	}
}
