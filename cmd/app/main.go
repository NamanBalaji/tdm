package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	message string
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m model) View() string {
	return fmt.Sprintf("Welcome to TDM!\n\n" + "Press 'q' to quit.\n")
}

func main() {
	initialModel := model{
		message: "Welcome to TDM!",
	}

	if _, err := tea.NewProgram(initialModel).Run(); err != nil {
		fmt.Println("Error running TUI program:", err)
		os.Exit(1)
	}
}
