package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/NamanBalaji/tdm/internal/engine"
)

// Run initializes and starts the TUI.
func Run(eng *engine.Engine) error {
	m := NewModel(eng)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()

	return err
}
