package components

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// DownloadItem renders a single download entry given its info, width, and selection state.
func DownloadItem(info engine.DownloadInfo, width int, selected bool) string {
	// --- Filename ---
	// CORRECT: Get the filename from the DownloadInfo struct.
	name := info.Filename

	maxNameLen := 40
	if len(name) > maxNameLen {
		name = name[:maxNameLen-3] + "..."
	}

	// --- Status Label ---
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

	// --- Progress Percentage ---
	percentVal := info.Progress.GetPercentage()
	percent := fmt.Sprintf("%.1f%%", percentVal)
	percentStyle := lipgloss.NewStyle().Width(8).Align(lipgloss.Right)
	formattedPercent := percentStyle.Render(percent)

	// --- Line 1: Name, Status, Percentage ---
	nameWidth := maxNameLen
	statusWidth := lipgloss.Width(statusLabel)

	remainingSpace := width - nameWidth - statusWidth - lipgloss.Width(formattedPercent) - 3
	if remainingSpace < 2 {
		remainingSpace = 2
	}

	padding := strings.Repeat(" ", remainingSpace)
	line1 := fmt.Sprintf("%-*s %s%s%s", nameWidth, name, statusLabel, padding, formattedPercent)

	// --- Line 2: Progress Bar ---
	barWidth := width - 2
	if barWidth < 10 {
		barWidth = 10
	}

	bar := ProgressBar(barWidth, percentVal/100.0, info.Status)
	line2 := styles.ListItemStyle.Render(bar)

	// --- Line 3: Size, Speed, ETA ---
	sizeInfo := fmt.Sprintf("%s / %s", formatSize(info.Progress.GetDownloaded()), formatSize(info.Progress.GetTotalSize()))

	speedInfo := "--/s"
	if info.Status == status.Active {
		// CORRECT: GetSpeedBPS now returns int64, so no conversion is needed.
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

	// --- Combine and Style ---
	item := lipgloss.JoinVertical(lipgloss.Left, line1, line2, line3)
	if selected {
		return styles.SelectedItemStyle.Width(width).Render(item)
	}

	return styles.ListItemStyle.Width(width).Render(item)
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
