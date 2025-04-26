package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// DownloadItem renders a single download entry given its stats, the available width, and selection state.
func DownloadItem(stats downloader.Stats, width int, selected bool) string {
	name := stats.Filename
	maxNameLen := 30
	if len(name) > maxNameLen {
		name = name[:maxNameLen-3] + "..."
	}

	var progressPercent float64
	if stats.TotalSize > 0 {
		progressPercent = float64(stats.Downloaded) / float64(stats.TotalSize)
	}

	if stats.Status == common.StatusCompleted {
		progressPercent = 1.0
	}

	percent := fmt.Sprintf("%.1f%%", progressPercent*100)

	var statusLabel string
	switch stats.Status {
	case common.StatusActive:
		statusLabel = styles.StatusActive.Render("● active")
	case common.StatusQueued:
		statusLabel = styles.StatusQueued.Render("○ queued")
	case common.StatusPaused:
		statusLabel = styles.StatusPaused.Render("❚❚ paused")
	case common.StatusCompleted:
		statusLabel = styles.StatusCompleted.Render("✔ completed")
	case common.StatusCancelled:
		statusLabel = styles.StatusCancelled.Render("⊘ cancelled")
	case common.StatusFailed:
		statusLabel = styles.StatusFailed.Render("✖ failed")
	default:
		statusLabel = styles.StatusFailed.Render("unknown")
	}

	nameWidth := maxNameLen
	statusWidth := lipgloss.Width(statusLabel)

	percentStyle := lipgloss.NewStyle().Width(10).Align(lipgloss.Right)
	formattedPercent := percentStyle.Render(percent)

	remainingSpace := width - nameWidth - statusWidth - lipgloss.Width(formattedPercent) - 3
	if remainingSpace < 2 {
		remainingSpace = 2
	}

	padding := strings.Repeat(" ", remainingSpace)

	line1 := fmt.Sprintf("%-*s %s%s%s",
		nameWidth,
		name,
		statusLabel,
		padding,
		formattedPercent)

	barWidth := width - 2
	if barWidth < 10 {
		barWidth = 10
	}

	bar := ProgressBar(barWidth, progressPercent, stats.Status)
	line2 := styles.ListItemStyle.Render(bar)

	var totalSize = stats.TotalSize
	var downloaded = stats.Downloaded

	if stats.Status == common.StatusCompleted && totalSize > 0 {
		downloaded = totalSize
	}

	sizeInfo := fmt.Sprintf("%s / %s", formatSize(downloaded), formatSize(totalSize))

	speedInfo := "--/s"
	if stats.Status == common.StatusActive {
		speedInfo = formatSize(stats.Speed) + "/s"
	}

	eta := "--"
	if stats.Status == common.StatusActive && stats.TimeRemaining > 0 {
		eta = formatDuration(stats.TimeRemaining)
	} else if stats.Status == common.StatusCompleted {
		eta = "Completed"
	}

	info := fmt.Sprintf("%s  %s  ETA %s", sizeInfo, speedInfo, eta)
	line3 := styles.ListItemStyle.Faint(true).Render(info)

	item := lipgloss.JoinVertical(lipgloss.Left, line1, line2, line3)
	if selected {
		return styles.SelectedItemStyle.Width(width).Render(item)
	}
	return styles.ListItemStyle.Width(width).Render(item)
}

// formatSize converts bytes into a human-readable string.
func formatSize(bytes int64) string {
	const unit = 1000
	if bytes < 0 {
		return "Unknown"
	}
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	d := float64(bytes)
	exp := 0
	for d >= unit {
		d /= unit
		exp++
	}
	prefixes := "kMGTPE"
	idx := exp - 1
	if idx < 0 {
		idx = 0
	} else if idx >= len(prefixes) {
		idx = len(prefixes) - 1
	}

	return fmt.Sprintf("%.1f %cB", d, prefixes[idx])
}

// formatDuration returns a more user-friendly duration string.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		m := d / time.Minute
		s := (d % time.Minute) / time.Second
		return fmt.Sprintf("%dm %ds", m, s)
	} else {
		h := d / time.Hour
		m := (d % time.Hour) / time.Minute
		return fmt.Sprintf("%dh %dm", h, m)
	}
}
