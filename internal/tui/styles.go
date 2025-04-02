package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	gruvboxBg0    = lipgloss.Color("#282828")
	gruvboxBg1    = lipgloss.Color("#3c3836")
	gruvboxBg2    = lipgloss.Color("#504945")
	gruvboxFg0    = lipgloss.Color("#fbf1c7")
	gruvboxFg1    = lipgloss.Color("#ebdbb2")
	gruvboxFg2    = lipgloss.Color("#d5c4a1")
	gruvboxRed    = lipgloss.Color("#fb4934")
	gruvboxGreen  = lipgloss.Color("#b8bb26")
	gruvboxYellow = lipgloss.Color("#fabd2f")
	gruvboxBlue   = lipgloss.Color("#83a598")
	gruvboxPurple = lipgloss.Color("#d3869b")
	gruvboxAqua   = lipgloss.Color("#8ec07c")
	gruvboxOrange = lipgloss.Color("#fe8019")
)

// Styles
var (
	// Base application styles
	appStyle = lipgloss.NewStyle().
			Background(gruvboxBg0).
			Foreground(gruvboxFg1)

	// Header styles
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(gruvboxYellow).
			Background(gruvboxBg1).
			Padding(1, 2).
			Width(80).
			Align(lipgloss.Center)

	// Download item styles
	downloadItemStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Width(80)

	selectedDownloadStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(gruvboxYellow).
				Padding(0, 1)

	progressBarFilledStyle = lipgloss.NewStyle().
				Foreground(gruvboxGreen)

	progressBarEmptyStyle = lipgloss.NewStyle().
				Foreground(gruvboxBg2)

	statusStyleActive = lipgloss.NewStyle().
				Foreground(gruvboxGreen).
				Bold(true)

	statusStyleQueued = lipgloss.NewStyle().
				Foreground(gruvboxYellow).
				Bold(true)

	statusStylePaused = lipgloss.NewStyle().
				Foreground(gruvboxOrange).
				Bold(true)

	statusStyleCompleted = lipgloss.NewStyle().
				Foreground(gruvboxBlue).
				Bold(true)

	statusStyleFailed = lipgloss.NewStyle().
				Foreground(gruvboxRed).
				Bold(true)

	// Add download form styles
	formLabelStyle = lipgloss.NewStyle().
			Foreground(gruvboxFg0).
			MarginRight(1)

	formInputStyle = lipgloss.NewStyle().
			Foreground(gruvboxFg1).
			Background(gruvboxBg1).
			Padding(0, 1)

	// Help styles
	helpStyle = lipgloss.NewStyle().
			Foreground(gruvboxFg2)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(gruvboxYellow).
			Bold(true)

	// Error message style
	errorStyle = lipgloss.NewStyle().
			Foreground(gruvboxBg0).
			Background(gruvboxRed).
			Padding(0, 1).
			Margin(1, 0).
			Width(80).
			Align(lipgloss.Center)
)
