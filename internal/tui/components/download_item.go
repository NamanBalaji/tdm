package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

func DownloadItem(info engine.DownloadInfo, width int, selected bool) string {
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
	case status.Active:
		statusLabel = styles.StatusActive.Render("● active")
	case status.Paused:
		statusLabel = styles.StatusPaused.Render("❚❚ paused")
	case status.Completed:
		statusLabel = styles.StatusCompleted.Render("✔ completed")
	case status.Cancelled:
		statusLabel = styles.StatusCancelled.Render("⊘ cancelled")
	case status.Failed:
		statusLabel = styles.StatusFailed.Render("✖ failed")
	default: // Queued
		statusLabel = styles.StatusQueued.Render("○ queued")
	}

	statusBlock := lipgloss.NewStyle().Width(12).Render(statusLabel)

	percentVal := info.Progress.GetPercentage()
	percent := fmt.Sprintf("%.1f%%", percentVal)

	spacer := lipgloss.NewStyle().Width(innerWidth - nameWidth - lipgloss.Width(statusBlock) - lipgloss.Width(percent)).Render("")
	line1 := lipgloss.JoinHorizontal(lipgloss.Bottom, nameBlock, statusBlock, spacer, percent)

	bar := ProgressBar(innerWidth, info.Progress.GetPercentage()/100.0, info.Status)
	line2 := styles.ListItemStyle.Render(bar)

	sizeInfo := fmt.Sprintf("%s / %s", formatSize(info.Progress.GetDownloaded()), formatSize(info.Progress.GetTotalSize()))

	speedInfo := "--/s"
	if info.Status == status.Active {
		speedInfo = formatSize(info.Progress.GetSpeedBPS()) + "/s"
	}

	eta := "--"
	if info.Status == status.Active && info.Progress.GetETA() != "unknown" {
		eta = info.Progress.GetETA()
	} else if info.Status == status.Completed {
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
