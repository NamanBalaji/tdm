package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// ConfirmDialogModel represents a simple confirmation dialog.
type ConfirmDialogModel struct {
	title    string
	message  string
	action   string // "cancel" or "remove"
	targetID uuid.UUID
	width    int
}

// View renders the confirmation dialog.
func (c ConfirmDialogModel) View() string {
	var s strings.Builder

	// Header
	s.WriteString(lipgloss.NewStyle().Bold(true).Foreground(catpRed).Width(c.width).Align(lipgloss.Center).Render(c.title))
	s.WriteString("\n\n")

	// Message
	s.WriteString(lipgloss.NewStyle().Foreground(catpYellow).Align(lipgloss.Center).Width(c.width).Render(c.message))
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(lipgloss.NewStyle().Foreground(catpText).Align(lipgloss.Center).Width(c.width).Render("Press Enter to confirm or Esc to cancel"))

	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(catpRed).Padding(1, 2).Render(s.String())
}
