package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/tui/components"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// RenderDownloadList displays the list of download items within the given width and height.
func RenderDownloadList(m *Model, width, height int) string {
	var rows []string

	availableHeight := height
	if availableHeight < 1 {
		return ""
	}

	itemHeight := 3
	visible := availableHeight / (itemHeight + 1)
	if visible < 1 {
		visible = 1
	}

	start := m.selected - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(m.stats) {
		end = len(m.stats)
		start = end - visible
		if start < 0 {
			start = 0
		}
	}

	for i := start; i < end; i++ {
		item := components.DownloadItem(m.stats[i], width, i == m.selected)

		if i > start {
			rows = append(rows, lipgloss.NewStyle().Height(1).Render(""))
		}

		rows = append(rows, item)
	}

	if start > 0 {
		upIndicator := lipgloss.NewStyle().
			Foreground(styles.Subtext0).
			Align(lipgloss.Center).
			Width(width).
			Render("↑ more above")
		rows = append([]string{upIndicator, ""}, rows...)
	}

	if end < len(m.stats) {
		downIndicator := lipgloss.NewStyle().
			Foreground(styles.Subtext0).
			Align(lipgloss.Center).
			Width(width).
			Render("↓ more below")
		rows = append(rows, "", downIndicator)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
