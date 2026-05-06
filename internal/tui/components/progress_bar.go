package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// ProgressBar returns a styled progress bar.
func ProgressBar(width int, percent float64, s download.Status) string {
	if width <= 0 {
		return ""
	}

	if percent < 0 {
		percent = 0
	}

	if percent > 1.0 {
		percent = 1.0
	}

	filledWidth := int(float64(width) * percent)
	emptyWidth := width - filledWidth

	filledStr := strings.Repeat("█", filledWidth)
	emptyStr := strings.Repeat("░", emptyWidth)

	var filledStyle lipgloss.Style

	switch s {
	case download.Active:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Teal)
	case download.Paused:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Peach)
	case download.Completed:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Green)
	case download.Cancelled:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Mauve)
	case download.Failed:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Red)
	case download.Pending, download.Queued:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Yellow)
	}

	bar := filledStyle.Render(filledStr) + styles.ProgressBarEmptyStyle.Render(emptyStr)

	return bar
}
