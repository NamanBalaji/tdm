package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// ProgressBar returns a styled progress bar.
func ProgressBar(width int, percent float64, s status.Status) string {
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
	case status.Active:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Teal)
	case status.Paused:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Peach)
	case status.Completed:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Green)
	case status.Cancelled:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Mauve)
	case status.Failed:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Red)
	default: // Queued or Pending
		filledStyle = lipgloss.NewStyle().Foreground(styles.Yellow)
	}

	bar := filledStyle.Render(filledStr) + styles.ProgressBarEmptyStyle.Render(emptyStr)

	return bar
}
