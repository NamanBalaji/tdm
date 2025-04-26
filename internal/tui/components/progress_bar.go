package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// ProgressBar returns a styled progress bar of given width and fill percentage.
func ProgressBar(width int, percent float64, status common.Status) string {
	if width <= 0 {
		return ""
	}

	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}

	if status == common.StatusCompleted {
		percent = 1.0
	}

	filled := int(percent * float64(width))
	if filled > width {
		filled = width
	}

	empty := width - filled

	filledStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", empty)

	var filledStyle lipgloss.Style
	switch status {
	case common.StatusActive:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Teal)
	case common.StatusQueued:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Yellow)
	case common.StatusPaused:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Peach)
	case common.StatusCompleted:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Green)
	case common.StatusCancelled:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Mauve)
	case common.StatusFailed:
		filledStyle = lipgloss.NewStyle().Foreground(styles.Red)
	default:
		filledStyle = styles.ProgressBarFilledStyle
	}

	var result string
	if filled > 0 {
		result = filledStyle.Render(filledStr)
	}
	if empty > 0 {
		result += styles.ProgressBarEmptyStyle.Render(emptyStr)
	}

	return result
}
