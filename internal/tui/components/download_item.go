package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

func DownloadItem(info download.DownloadInfo, width int, selected bool) string {
	const horizontalPadding = 4

	innerWidth := width - horizontalPadding

	name := info.Filename

	nameWidth := int(float64(innerWidth) * 0.6)
	if len(name) > nameWidth {
		name = name[:nameWidth-3] + "..."
	}

	nameBlock := lipgloss.NewStyle().Width(nameWidth).Render(name)

	var statusLabel string

	switch info.Status {
	case download.Active:
		statusLabel = styles.StatusActive.Render("● active")
	case download.Paused:
		statusLabel = styles.StatusPaused.Render("❚❚ paused")
	case download.Completed:
		statusLabel = styles.StatusCompleted.Render("✔ completed")
	case download.Cancelled:
		statusLabel = styles.StatusCancelled.Render("⊘ cancelled")
	case download.Failed:
		statusLabel = styles.StatusFailed.Render("✖ failed")
	case download.Pending, download.Queued:
		statusLabel = styles.StatusQueued.Render("○ queued")
	}

	statusBlock := lipgloss.NewStyle().Width(12).Render(statusLabel)

	percent := fmt.Sprintf("%.1f%%", info.Progress.Percentage)

	spacer := lipgloss.NewStyle().Width(max(innerWidth-nameWidth-lipgloss.Width(statusBlock)-lipgloss.Width(percent), 0)).Render("")
	line1 := lipgloss.JoinHorizontal(lipgloss.Bottom, nameBlock, statusBlock, spacer, percent)

	bar := ProgressBar(innerWidth, info.Progress.Percentage/100.0, info.Status)
	line2 := styles.ListItemStyle.Render(bar)

	sizeInfo := fmt.Sprintf("%s / %s", formatSize(info.Progress.Downloaded), formatSize(info.Progress.TotalSize))

	speedInfo := "--/s"
	if info.Status == download.Active {
		speedInfo = formatSize(info.Progress.SpeedBPS) + "/s"
	}

	eta := "--"
	if info.Status == download.Active && info.Progress.ETA > 0 {
		eta = info.Progress.ETA.String()
	} else if info.Status == download.Completed {
		eta = "Done"
	}

	infoLine := fmt.Sprintf("%s  %s  ETA: %s", sizeInfo, speedInfo, eta)
	line3 := styles.ListItemStyle.Faint(true).Render(infoLine)

	item := lipgloss.JoinVertical(lipgloss.Left, line1, line2, line3)

	containerStyle := styles.ListItemStyle
	if selected {
		containerStyle = styles.SelectedItemStyle
	}

	return containerStyle.Padding(0, 2).Width(width).Render(item)
}

// formatSize converts bytes into a human-readable string.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
