package components_test

import (
	"strings"
	"testing"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/components"
	"github.com/google/uuid"
)

func TestRenderDownloadList(t *testing.T) {
	downloads := []engine.DownloadInfo{
		{ID: uuid.New(), Filename: "file-0.txt", Status: status.Active, Progress: progress.Progress{Percentage: 10}},
		{ID: uuid.New(), Filename: "file-1.txt", Status: status.Paused, Progress: progress.Progress{Percentage: 20}},
		{ID: uuid.New(), Filename: "file-2.txt", Status: status.Completed, Progress: progress.Progress{Percentage: 100}},
		{ID: uuid.New(), Filename: "file-3.txt", Status: status.Queued, Progress: progress.Progress{Percentage: 0}},
		{ID: uuid.New(), Filename: "file-4.txt", Status: status.Failed, Progress: progress.Progress{Percentage: 50}},
	}

	testCases := []struct {
		name             string
		downloads        []engine.DownloadInfo
		selected         int
		width            int
		height           int
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:          "Empty list",
			downloads:     []engine.DownloadInfo{},
			selected:      0,
			width:         80,
			height:        20,
			shouldContain: []string{"Terminal Download Manager"}, // Check for empty view content
		},
		{
			name:          "List with items, no scrolling needed",
			downloads:     downloads[:2],
			selected:      1,
			width:         80,
			height:        10, // Enough height for 2 items (4 rows each) + buffer
			shouldContain: []string{"file-0.txt", "file-1.txt"},
		},
		{
			name:             "Scrolling down, selected item in middle",
			downloads:        downloads,
			selected:         2,
			width:            80,
			height:           12, // Height for 3 items
			shouldContain:    []string{"file-1.txt", "file-2.txt", "file-3.txt"},
			shouldNotContain: []string{"file-0.txt", "file-4.txt"},
		},
		{
			name:             "Scrolling to top, selected first item",
			downloads:        downloads,
			selected:         0,
			width:            80,
			height:           12, // Height for 3 items
			shouldContain:    []string{"file-0.txt", "file-1.txt", "file-2.txt"},
			shouldNotContain: []string{"file-3.txt", "file-4.txt"},
		},
		{
			name:             "Scrolling to bottom, selected last item",
			downloads:        downloads,
			selected:         4,
			width:            80,
			height:           12, // Height for 3 items
			shouldContain:    []string{"file-2.txt", "file-3.txt", "file-4.txt"},
			shouldNotContain: []string{"file-0.txt", "file-1.txt"},
		},
		{
			name:          "Zero height",
			downloads:     downloads,
			selected:      0,
			width:         80,
			height:        0,
			shouldContain: []string{""}, // Should produce an empty string
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := components.RenderDownloadList(tc.downloads, tc.selected, tc.width, tc.height)

			for _, check := range tc.shouldContain {
				if check == "" {
					if strings.TrimSpace(output) != "" {
						t.Errorf("expected visually empty output, but got %q", output)
					}
				} else if !strings.Contains(output, check) {
					t.Errorf("expected output to contain %q, but it did not.\nOutput:\n%s", check, output)
				}
			}

			for _, check := range tc.shouldNotContain {
				if strings.Contains(output, check) {
					t.Errorf("expected output to NOT contain %q, but it did.\nOutput:\n%s", check, output)
				}
			}
		})
	}
}
