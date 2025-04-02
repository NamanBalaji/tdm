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
		progress.WithSolidFill(string(gruvboxGreen)),
	)

	stats := download.GetStats()
	if stats.Status == common.StatusActive {
		p.FullColor = "76"
		p.EmptyColor = "237"
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(gruvboxGreen)

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

	location := lipgloss.NewStyle().
		Foreground(gruvboxFg2). // Keep existing color
		Faint(true). // Add this
		Render(d.download.Config.Directory)

	var statusStr string
	switch stats.Status {
	case common.StatusActive:
		statusStr = statusStyleActive.Render(fmt.Sprintf("%s %s", d.spinner.View(), string(stats.Status)))
		d.progress.FullColor = "76"
		d.progress.EmptyColor = "237"
	case common.StatusQueued:
		statusStr = statusStyleQueued.Render(string(stats.Status))
		d.progress.FullColor = "4"
		d.progress.EmptyColor = "236"
	case common.StatusPaused:
		statusStr = statusStylePaused.Render(string(stats.Status))
		d.progress.FullColor = "4"
		d.progress.EmptyColor = "236"
	case common.StatusCompleted:
		statusStr = statusStyleCompleted.Render(string(stats.Status))
		d.progress.FullColor = "4"
		d.progress.EmptyColor = "236"
	case common.StatusFailed, common.StatusCancelled:
		statusStr = statusStyleFailed.Render(string(stats.Status))
		d.progress.FullColor = "4"
		d.progress.EmptyColor = "236"
	default:
		statusStr = string(stats.Status)
	}

	firstLine := fmt.Sprintf("%-30s  %s", filename, statusStr)

	secondLine := location

	progressBar := customProgressBar(d.progress.Width, stats.Progress/100.0)

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
func customProgressBar(width int, percent float64) string {
	w := float64(width)

	filled := int(w * percent)
	empty := width - filled

	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	return progressBarFilledStyle.Render(filledStr) +
		progressBarEmptyStyle.Render(emptyStr)
}

// AddDownloadModel represents the add download form
type AddDownloadModel struct {
	textInput textinput.Model
	width     int
}

// View renders the add download form
func (a AddDownloadModel) View() string {
	formWidth := a.width - 10
	if formWidth < 40 {
		formWidth = 40
	}

	return lipgloss.NewStyle().
		Width(formWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(gruvboxYellow).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				formLabelStyle.Render("Enter URL to download:"),
				a.textInput.View(),
				"",
				lipgloss.NewStyle().Foreground(gruvboxFg2).Render("Press Enter to confirm or Esc to cancel"),
			),
		)
}
