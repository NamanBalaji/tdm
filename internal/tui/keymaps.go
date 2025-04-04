package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// Keymap defines the keys for the application
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Add      key.Binding
	Remove   key.Binding
	Pause    key.Binding
	Resume   key.Binding
	Cancel   key.Binding
	Back     key.Binding
	Confirm  key.Binding
	Quit     key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Pause, k.Resume, k.Remove, k.Cancel, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Add},
		{k.Pause, k.Resume, k.Cancel, k.Remove},
		{k.Back, k.Confirm, k.Quit},
	}
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup/b", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f"), key.WithHelp("pgdown/f", "page down")),
		Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add download")),
		Remove:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove download")),
		Pause:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
		Resume:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "resume")),
		Cancel:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
