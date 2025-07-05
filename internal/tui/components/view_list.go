package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

func RenderDownloadList(downloads []engine.DownloadInfo, selected int, width, height int) string {
	if len(downloads) == 0 {
		return renderEmptyView(width, height)
	}

	if height <= 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render("")
	}

	var rows []string

	itemHeight := 4

	visibleCount := height / itemHeight

	start := selected - (visibleCount / 2)
	if start < 0 {
		start = 0
	}

	end := start + visibleCount
	if end > len(downloads) {
		end = len(downloads)

		start = end - visibleCount
		if start < 0 {
			start = 0
		}
	}

	for i := start; i < end; i++ {
		item := DownloadItem(downloads[i], width, i == selected)
		rows = append(rows, item)
	}

	listContent := lipgloss.JoinVertical(lipgloss.Left, rows...)

	return lipgloss.NewStyle().Width(width).Height(height).Render(listContent)
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
