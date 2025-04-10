package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Catppuccin Mocha color palette.
var (
		// Base colors.
	catpBase     = lipgloss.Color("#1e1e2e") // Base
	catpCrust    = lipgloss.Color("#11111b") // Crust (darkest)
	catpText     = lipgloss.Color("#cdd6f4") // Text
	catpSubtext0 = lipgloss.Color("#a6adc8") // Subtext 0
	catpSurface0 = lipgloss.Color("#313244") // Surface 0

	catpPink     = lipgloss.Color("#f5c2e7") // Pink
	catpMauve    = lipgloss.Color("#cba6f7") // Mauve (purple)
	catpRed      = lipgloss.Color("#f38ba8") // Red
	catpPeach    = lipgloss.Color("#fab387") // Peach
	catpYellow   = lipgloss.Color("#f9e2af") // Yellow
	catpGreen    = lipgloss.Color("#a6e3a1") // Green
	catpTeal     = lipgloss.Color("#94e2d5") // Teal
	catpSapphire = lipgloss.Color("#74c7ec") // Sapphire
	catpBlue     = lipgloss.Color("#89b4fa") // Blue
	catpLavender = lipgloss.Color("#b4befe") // Lavender
)

// Styles.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(catpCrust).Background(catpPink).Padding(1, 2).Width(80).Align(lipgloss.Center)

	downloadItemStyle = lipgloss.NewStyle().Padding(0, 1).Width(80)

	selectedDownloadStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(catpPink).Padding(0, 1)

	progressBarFilledStyle = lipgloss.NewStyle().Foreground(catpGreen)

	progressBarEmptyStyle = lipgloss.NewStyle().Foreground(catpSurface0)

	statusStyleActive = lipgloss.NewStyle().Foreground(catpTeal).Bold(true)

	statusStyleQueued = lipgloss.NewStyle().Foreground(catpYellow).Bold(true)

	statusStylePaused = lipgloss.NewStyle().Foreground(catpPeach).Bold(true)

	statusStyleCompleted = lipgloss.NewStyle().Foreground(catpGreen).Bold(true)

	statusStyleFailed = lipgloss.NewStyle().Foreground(catpRed).Bold(true)

	formLabelStyle = lipgloss.NewStyle().Foreground(catpSapphire).MarginRight(1)

	errorStyle = lipgloss.NewStyle().Foreground(catpBase).Background(catpRed).Padding(0, 1).Margin(1, 0).Width(80).Align(lipgloss.Center)
)
