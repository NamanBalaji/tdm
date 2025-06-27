package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/tui/components"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

type currentView int

const (
	viewList currentView = iota
	viewAddURL
	viewAddPriority
	viewConfirmRemove
	viewConfirmCancel
)

// Model is the main TUI application model.
type Model struct {
	engine *engine.Engine
	view   currentView

	list          listModel
	urlInput      textinput.Model
	priorityInput textinput.Model
	spinner       spinner.Model
	help          help.Model
	keys          keyMap

	width, height int
	errMsg        string
	successMsg    string
	lastRefresh   time.Time
	loaded        bool
}

type listModel struct {
	downloads []engine.DownloadInfo
	selected  int
}

type (
	clearMsg       struct{}
	tickMsg        struct{}
	downloadsMsg   []engine.DownloadInfo
	downloadErrMsg struct{ error }
)

func clearNotifications() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearMsg{}
	})
}

// NewModel creates a new TUI model.
func NewModel(e *engine.Engine) Model {
	urlInput := textinput.New()
	urlInput.Placeholder = "Enter download URL"
	urlInput.Focus()
	urlInput.CharLimit = 1024
	urlInput.Width = 60

	priorityInput := textinput.New()
	priorityInput.Placeholder = "Priority (1-10, default 5)"
	priorityInput.CharLimit = 2
	priorityInput.Width = 30
	priorityInput.Validate = func(s string) error {
		if s == "" {
			return nil
		}

		p, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("must be a number")
		}

		if p < 1 || p > 10 {
			return errors.New("must be between 1 and 10")
		}

		return nil
	}

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(styles.Pink)

	return Model{
		engine:        e,
		view:          viewList,
		urlInput:      urlInput,
		priorityInput: priorityInput,
		spinner:       sp,
		help:          help.New(),
		keys:          newKeyMap(),
		lastRefresh:   time.Now().Add(-10 * time.Second),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshDownloads(),
		spinner.Tick,
		tea.Every(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg{}
		}),
	)
}

func (m Model) refreshDownloads() tea.Cmd {
	return func() tea.Msg {
		return downloadsMsg(m.engine.GetAllDownloads())
	}
}

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width

	case tickMsg:
		cmds = append(cmds, m.refreshDownloads())

		return m, tea.Batch(
			tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} }),
			tea.Batch(cmds...),
		)

	case downloadsMsg:
		m.list.downloads = msg
		if !m.loaded {
			m.loaded = true
		}

		if m.list.selected >= len(m.list.downloads) {
			m.list.selected = len(m.list.downloads) - 1
		}

		if m.list.selected < 0 {
			m.list.selected = 0
		}

		return m, nil

	case downloadErrMsg:
		m.errMsg = msg.Error()
		return m, clearNotifications()

	case clearMsg:
		m.errMsg = ""
		m.successMsg = ""

		return m, nil

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// Global keybindings
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
	}

	switch m.view {
	case viewList:
		cmd = m.updateListView(msg)
	case viewAddURL:
		cmd = m.updateAddURLView(msg)
	case viewAddPriority:
		cmd = m.updateAddPriorityView(msg)
	case viewConfirmRemove:
		cmd = m.updateConfirmRemoveView(msg)
	case viewConfirmCancel:
		cmd = m.updateConfirmCancelView(msg)
	}

	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) getSelectedDownloadID() (uuid.UUID, bool) {
	if len(m.list.downloads) > 0 && m.list.selected < len(m.list.downloads) {
		return m.list.downloads[m.list.selected].ID, true
	}

	return uuid.Nil, false
}

func (m *Model) updateListView(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.list.selected > 0 {
				m.list.selected--
			}
		case key.Matches(msg, m.keys.Down):
			if m.list.selected < len(m.list.downloads)-1 {
				m.list.selected++
			}
		case key.Matches(msg, m.keys.Add):
			m.view = viewAddURL
			m.urlInput.Focus()

			return textinput.Blink
		case key.Matches(msg, m.keys.Pause):
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.engine.PauseDownload(id)
			}
		case key.Matches(msg, m.keys.Resume):
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.engine.ResumeDownload(context.Background(), id)
			}
		case key.Matches(msg, m.keys.Cancel):
			if _, ok := m.getSelectedDownloadID(); ok {
				m.view = viewConfirmCancel
			}
		case key.Matches(msg, m.keys.Remove):
			if _, ok := m.getSelectedDownloadID(); ok {
				m.view = viewConfirmRemove
			}
		}
	}

	return nil
}

func (m *Model) updateAddURLView(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	m.urlInput, cmd = m.urlInput.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Confirm):
			if m.urlInput.Value() != "" {
				m.view = viewAddPriority
				m.priorityInput.Focus()

				return textinput.Blink
			}
		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			m.urlInput.SetValue("")
		}
	}

	return cmd
}

func (m *Model) updateAddPriorityView(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	m.priorityInput, cmd = m.priorityInput.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Confirm):
			if m.priorityInput.Validate(m.priorityInput.Value()) == nil {
				priority := 5 // Default priority

				if m.priorityInput.Value() != "" {
					p, _ := strconv.Atoi(m.priorityInput.Value())
					priority = p
				}

				url := m.urlInput.Value()

				go func() {
					_, err := m.engine.AddDownload(context.Background(), url, priority)
					if err != nil {
						// This should be handled by a message from the engine
					}
				}()

				m.successMsg = "Added download for: " + url
				m.view = viewList
				m.urlInput.SetValue("")
				m.priorityInput.SetValue("")

				return clearNotifications()
			}
		case key.Matches(msg, m.keys.Back):
			m.view = viewAddURL
			m.priorityInput.SetValue("")
			m.urlInput.Focus()

			return textinput.Blink
		}
	}

	return cmd
}

func (m *Model) updateConfirmRemoveView(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.engine.RemoveDownload(id)
			}

			m.view = viewList

			return m.refreshDownloads()
		case "n", "N", "esc":
			m.view = viewList
			return nil
		}
	}

	return nil
}

func (m *Model) updateConfirmCancelView(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.engine.CancelDownload(id)
			}

			m.view = viewList

			return m.refreshDownloads()
		case "n", "N", "esc":
			m.view = viewList
			return nil
		}
	}

	return nil
}

// View renders the TUI.
func (m Model) View() string {
	if !m.loaded {
		return fmt.Sprintf("\n  %s Loading downloads... Please wait.\n\n", m.spinner.View())
	}

	mainContent := ""

	switch m.view {
	case viewList:
		// CORRECTED: Call the component function.
		mainContent = components.RenderDownloadList(m.list.downloads, m.list.selected, m.width, m.height-10)
	case viewAddURL, viewAddPriority:
		mainContent = m.renderAddView()
	case viewConfirmRemove:
		mainContent = m.renderConfirmDialog("Are you sure you want to remove this download? (y/n)")
	case viewConfirmCancel:
		mainContent = m.renderConfirmDialog("Are you sure you want to cancel this download? (y/n)")
	}

	header := renderHeader(&m)
	footer := styles.FooterStyle.Width(m.width).Render(m.help.View(m.keys))
	notification := m.renderNotification()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		notification,
		mainContent,
		footer,
	)
}

func (m *Model) renderAddView() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(styles.Pink).Render("Add New Download")
	b.WriteString(title)
	b.WriteString("\n\n" + m.urlInput.View())
	b.WriteString("\n\n" + m.priorityInput.View())

	err := m.priorityInput.Validate(m.priorityInput.Value())
	if err != nil {
		b.WriteString("\n" + styles.ErrorStyle.Render(err.Error()))
	}

	b.WriteString("\n\n(esc to go back)")

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Pink).
		Padding(1, 2).
		Render(b.String())

	return lipgloss.Place(m.width, m.height-10, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) renderConfirmDialog(prompt string) string {
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Red).
		Padding(1, 2).
		Render(prompt)

	return lipgloss.Place(m.width, m.height-10, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) renderNotification() string {
	if m.errMsg != "" {
		return styles.ErrorStyle.Width(m.width).Align(lipgloss.Center).Render(m.errMsg)
	}

	if m.successMsg != "" {
		return styles.SuccessStyle.Width(m.width).Align(lipgloss.Center).Render(m.successMsg)
	}

	return lipgloss.NewStyle().Height(1).Render("") // Placeholder for alignment
}

func renderHeader(m *Model) string {
	title := "TDM - Terminal Download Manager"
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Crust).
		Background(styles.Pink).
		Padding(0, 1).
		Width(m.width).
		Align(lipgloss.Center)
	header := headerStyle.Render(title)

	active := 0
	paused := 0
	completed := 0
	failed := 0

	for _, d := range m.list.downloads {
		switch d.Status {
		case 1: // Active
			active++
		case 2: // Paused
			paused++
		case 3: // Completed
			completed++
		case 4: // Failed
			failed++
		}
	}

	statsText := fmt.Sprintf(
		"Total: %d | Active: %d | Paused: %d | Completed: %d | Failed: %d",
		len(m.list.downloads), active, paused, completed, failed,
	)

	statsStyle := lipgloss.NewStyle().
		Foreground(styles.Text).
		Background(styles.Surface0).
		Padding(0, 1).
		Width(m.width).
		Align(lipgloss.Center)
	stats := statsStyle.Render(statsText)

	return lipgloss.JoinVertical(lipgloss.Top, header, stats)
}
