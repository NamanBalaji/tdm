package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// RenderDownloadList displays the list of download items.
func RenderDownloadList(downloads []engine.DownloadInfo, selected int, width, height int) string {
	if len(downloads) == 0 {
		return renderEmptyView(width, height)
	}

	var rows []string
	// Each item takes 3 lines + 1 separator
	itemHeight := 4

	visibleCount := height / itemHeight
	if visibleCount < 1 {
		visibleCount = 1
	}

	start := selected - (visibleCount / 2)
	if start < 0 {
		start = 0
	}

	end := start + visibleCount
	if end > len(downloads) {
		end = len(downloads)
	}

	for i := start; i < end; i++ {
		item := DownloadItem(downloads[i], width-4, i == selected)
		rows = append(rows, item)
	}

	listContent := lipgloss.JoinVertical(lipgloss.Left, rows...)

	if start > 0 {
		upIndicator := lipgloss.NewStyle().
			Foreground(styles.Subtext0).
			Align(lipgloss.Center).
			Width(width).
			Render("↑ more above")
		listContent = lipgloss.JoinVertical(lipgloss.Top, upIndicator, listContent)
	}

	if end < len(downloads) {
		downIndicator := lipgloss.NewStyle().
			Foreground(styles.Subtext0).
			Align(lipgloss.Center).
			Width(width).
			Render("↓ more below")
		listContent = lipgloss.JoinVertical(lipgloss.Bottom, listContent, downIndicator)
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(listContent)
}

// renderEmptyView displays the ASCII art and instructions when there are no downloads.
func renderEmptyView(width, height int) string {
	logo := []string{
		"████████╗██████╗ ███╗   ███╗",
		"╚══██╔══╝██╔══██╗████╗ ████║",
		"   ██║   ██║  ██║██╔████╔██║",
		"   ██║   ██║  ██║██║╚██╔╝██║",
		"   ██║   ██████╔╝██║ ╚═╝ ██║",
		"   ╚═╝   ╚═════╝ ╚═╝     ╚═╝",
	}
	colors := []lipgloss.Color{
		styles.Blue, styles.Mauve, styles.Red,
		styles.Peach, styles.Yellow, styles.Green,
	}

	var lines []string

	for i, line := range logo {
		styled := lipgloss.NewStyle().Foreground(colors[i]).Render(line)
		lines = append(lines, styled)
	}

	subtitle := lipgloss.NewStyle().Foreground(styles.Text).Italic(true).Render("Terminal Download Manager")
	instruction := lipgloss.NewStyle().Foreground(styles.Subtext0).Render("Press 'a' to add a download or 'q' to quit")
	content := lipgloss.JoinVertical(lipgloss.Center, lines...)
	content = lipgloss.JoinVertical(lipgloss.Center, content, "", subtitle, "", instruction)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
