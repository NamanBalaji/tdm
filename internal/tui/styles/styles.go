package styles

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	Base     = lipgloss.Color("#1e1e2e")
	Crust    = lipgloss.Color("#11111b")
	Text     = lipgloss.Color("#cdd6f4")
	Subtext0 = lipgloss.Color("#a6adc8")
	Surface0 = lipgloss.Color("#313244")

	Pink     = lipgloss.Color("#f5c2e7")
	Mauve    = lipgloss.Color("#cba6f7")
	Red      = lipgloss.Color("#f38ba8")
	Peach    = lipgloss.Color("#fab387")
	Yellow   = lipgloss.Color("#f9e2af")
	Green    = lipgloss.Color("#a6e3a1")
	Teal     = lipgloss.Color("#94e2d5")
	Sapphire = lipgloss.Color("#74c7ec")
	Blue     = lipgloss.Color("#89b4fa")
	Lavender = lipgloss.Color("#b4befe")
)

var (
	ErrorStyle = lipgloss.NewStyle().
			Foreground(Base).
			Background(Red).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Red).
			Padding(0, 1).
			Align(lipgloss.Center)

	ListItemStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(Text)

	SelectedItemStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(Pink).
				Padding(0, 1).
				Foreground(Text)

	ProgressBarEmptyStyle = lipgloss.NewStyle().Foreground(Surface0)

	StatusActive    = lipgloss.NewStyle().Foreground(Teal).Bold(true)
	StatusQueued    = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	StatusPaused    = lipgloss.NewStyle().Foreground(Peach).Bold(true)
	StatusCompleted = lipgloss.NewStyle().Foreground(Green).Bold(true)
	StatusCancelled = lipgloss.NewStyle().Foreground(Mauve).Bold(true)
	StatusFailed    = lipgloss.NewStyle().Foreground(Red).Bold(true)

	FooterStyle = lipgloss.NewStyle().
			Foreground(Subtext0).
			Padding(0, 1).
			Align(lipgloss.Center)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Base).
			Background(Green).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Green).
			Padding(0, 1).
			Align(lipgloss.Center)
)
