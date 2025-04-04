package tui

import (
	"fmt"
	"strings"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// DownloadModel represents a download in the TUI
type DownloadModel struct {
	download *downloader.Download
	progress progress.Model
	spinner  spinner.Model
	width    int
}

// NewDownloadModel creates a new download model for the TUI
func NewDownloadModel(download *downloader.Download) *DownloadModel {
	p := progress.New(
		progress.WithWidth(50),
		progress.WithoutPercentage(),
		progress.WithSolidFill(string(catpGreen)),
	)

	stats := download.GetStats()
	if stats.Status == common.StatusActive {
		p.FullColor = "76"
		p.EmptyColor = "237"
	}

	s := spinner.New()
	s.Spinner = spinner.Hamburger
	s.Style = lipgloss.NewStyle().Foreground(catpGreen)

	return &DownloadModel{
		download: download,
		progress: p,
		spinner:  s,
	}
}

// Update updates the download model with new stats
func (d *DownloadModel) Update(msg StatusUpdateMsg) {
	if msg.ID == d.download.ID {
		d.progress.SetPercent(float64(msg.Progress) / 100.0)
	}
}

// View renders the download model
func (d *DownloadModel) View() string {
	stats := d.download.GetStats()

	maxFilenameLen := 30
	filename := d.download.Filename
	if len(filename) > maxFilenameLen {
		filename = filename[:maxFilenameLen-3] + "..."
	}

	location := lipgloss.NewStyle().Foreground(catpSubtext0).Faint(true).Render(d.download.Config.Directory)

	var statusStr string
	switch stats.Status {
	case common.StatusActive:
		statusStr = statusStyleActive.Render(string(stats.Status))
		d.progress.FullColor = string(catpGreen)
		d.progress.EmptyColor = string(catpSurface0)
	case common.StatusQueued:
		statusStr = statusStyleQueued.Render(string(stats.Status))
		d.progress.FullColor = string(catpYellow)
		d.progress.EmptyColor = string(catpSurface0)
	case common.StatusPaused:
		statusStr = statusStylePaused.Render(string(stats.Status))
		d.progress.FullColor = string(catpPeach)
		d.progress.EmptyColor = string(catpSurface0)
	case common.StatusCompleted:
		statusStr = statusStyleCompleted.Render(string(stats.Status))
		d.progress.FullColor = string(catpGreen)
		d.progress.EmptyColor = string(catpSurface0)
	case common.StatusFailed, common.StatusCancelled:
		statusStr = statusStyleFailed.Render(string(stats.Status))
		d.progress.FullColor = string(catpRed)
		d.progress.EmptyColor = string(catpSurface0)
	default:
		statusStr = string(stats.Status)
	}

	firstLine := fmt.Sprintf("%-30s  %s", filename, statusStr)

	secondLine := location

	progressBar := customProgressBar(d.progress.Width, stats.Progress/100.0, lipgloss.Color(d.progress.FullColor), lipgloss.Color(d.progress.EmptyColor))

	var infoLine string
	if stats.Status == common.StatusActive {
		progressPercent := fmt.Sprintf("%.1f%%", stats.Progress)
		speedInfo := formatSpeed(stats.Speed)
		sizeInfo := fmt.Sprintf("%s / %s",
			formatSize(stats.Downloaded),
			formatSize(stats.TotalSize))

		infoLine = fmt.Sprintf("%s  %s  %s", progressPercent, sizeInfo, speedInfo)
	} else {
		// For non-active downloads, only show the size
		sizeInfo := fmt.Sprintf("%s / %s",
			formatSize(stats.Downloaded),
			formatSize(stats.TotalSize))

		infoLine = sizeInfo
	}

	return fmt.Sprintf("%s\n%s\n%s  %s", firstLine, secondLine, progressBar, infoLine)
}

// Helper function to format bytes to human-readable format
func formatSize(bytes int64) string {
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Helper function to format speed
func formatSpeed(bytesPerSec int64) string {
	if bytesPerSec < 0 {
		return "0 B/s"
	}
	return fmt.Sprintf("%s/s", formatSize(bytesPerSec))
}

// Custom progress bar render function
func customProgressBar(width int, percent float64, filledColor, emptyColor lipgloss.Color) string {
	w := float64(width)

	filled := int(w * percent)
	empty := width - filled

	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	return progressBarFilledStyle.Foreground(filledColor).Render(filledStr) +
		progressBarEmptyStyle.Foreground(emptyColor).Render(emptyStr)
}

// AddDownloadModel represents the add download form
type AddDownloadModel struct {
	textInput textinput.Model
	width     int
}

// View renders the add download form
func (a AddDownloadModel) View() string {
	formWidth := a.width - 4

	a.textInput.Width = formWidth - 4

	label := formLabelStyle.Render("Enter URL to download:")

	inputField := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(catpPink).Padding(0, 1).Width(formWidth).Render(a.textInput.View())

	instructions := lipgloss.NewStyle().Foreground(catpText).Render("Press Enter to confirm or Esc to cancel")

	return lipgloss.JoinVertical(lipgloss.Left, label, inputField, "", instructions)
}
