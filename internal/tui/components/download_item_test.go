package components_test

import (
	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/components"
	"github.com/google/uuid"
	"strings"
	"testing"
)

type mockProgress struct {
	totalSize  int64
	downloaded int64
	percentage float64
	speedBPS   int64
	eta        string
}

func (m *mockProgress) GetTotalSize() int64    { return m.totalSize }
func (m *mockProgress) GetDownloaded() int64   { return m.downloaded }
func (m *mockProgress) GetPercentage() float64 { return m.percentage }
func (m *mockProgress) GetSpeedBPS() int64     { return m.speedBPS }
func (m *mockProgress) GetETA() string         { return m.eta }

func TestDownloadItem(t *testing.T) {
	longFilename := "this-is-a-very-long-filename-that-will-definitely-be-truncated.zip"
	shortFilename := "file.txt"

	testCases := []struct {
		name           string
		info           engine.DownloadInfo
		width          int
		selected       bool
		expectedChecks []string
	}{
		{
			name: "Active Download",
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: shortFilename,
				Status:   status.Active,
				Progress: &mockProgress{
					totalSize:  1000,
					downloaded: 500,
					percentage: 50.0,
					speedBPS:   100,
					eta:        "5s",
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
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: shortFilename,
				Status:   status.Paused,
				Progress: &mockProgress{totalSize: 2048, downloaded: 1024, percentage: 50.0},
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
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: "completed.iso",
				Status:   status.Completed,
				Progress: &mockProgress{totalSize: 5000000, downloaded: 5000000, percentage: 100.0},
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
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: "failed_download",
				Status:   status.Failed,
				Progress: &mockProgress{totalSize: 1000, downloaded: 100, percentage: 10.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"failed_download", "failed", "10.0%"},
		},
		{
			name: "Cancelled Download",
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: "cancelled.tar.gz",
				Status:   status.Cancelled,
				Progress: &mockProgress{totalSize: 1000, downloaded: 200, percentage: 20.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"cancelled.tar.gz", "cancelled", "20.0%"},
		},
		{
			name: "Queued Download",
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: "queued_file",
				Status:   status.Queued,
				Progress: &mockProgress{totalSize: 5000, downloaded: 0, percentage: 0.0},
			},
			width:          80,
			selected:       false,
			expectedChecks: []string{"queued_file", "queued", "0.0%", "0 B / 4.9 KiB"},
		},
		{
			name: "Long Filename Truncation",
			info: engine.DownloadInfo{
				ID:       uuid.New(),
				Filename: longFilename,
				Status:   status.Active,
				Progress: &mockProgress{totalSize: 1000, downloaded: 10, percentage: 1.0},
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
