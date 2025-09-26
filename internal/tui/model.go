package tui

import (
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

var (
	ErrPriorityNAN   = errors.New("priority must be a number")
	ErrPriorityRange = errors.New("priority must be between 1 and 10")
)

const (
	viewList currentView = iota
	viewAdd
	viewConfirmRemove
	viewConfirmCancel
)

// Model is the main TUI application model.
type Model struct {
	actions           engineActions
	view              currentView
	addFormFocusIndex int

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
	clearMsg      struct{}
	tickMsg       struct{}
	downloadsMsg  []engine.DownloadInfo
	downloadError struct{ error }
)

func clearNotifications() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearMsg{}
	})
}

// NewModel creates a new TUI model.
func NewModel(actions engineActions) *Model {
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
			return ErrPriorityNAN
		}

		if p < 1 || p > 10 {
			return ErrPriorityRange
		}

		return nil
	}

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(styles.Pink)

	return &Model{
		actions:           actions,
		view:              viewList,
		addFormFocusIndex: 0,
		urlInput:          urlInput,
		priorityInput:     priorityInput,
		spinner:           sp,
		help:              help.New(),
		keys:              newKeyMap(),
		lastRefresh:       time.Now().Add(-10 * time.Second),
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshDownloads(),
		m.spinner.Tick,
		tea.Every(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg{}
		}),
	)
}

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case downloadError:
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
	case viewAdd:
		cmd = m.updateAddView(msg)
	case viewConfirmRemove:
		cmd = m.updateConfirmRemoveView(msg)
	case viewConfirmCancel:
		cmd = m.updateConfirmCancelView(msg)
	}

	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the TUI.
func (m *Model) View() string {
	if !m.loaded {
		return fmt.Sprintf("\n  %s Loading downloads... Please wait.\n\n", m.spinner.View())
	}

	header := renderHeader(m)
	footer := styles.FooterStyle.Width(m.width).Render(m.help.View(m.keys))
	notification := m.renderNotification()

	remainingHeight := m.height - lipgloss.Height(header) - lipgloss.Height(notification) - lipgloss.Height(footer)
	if remainingHeight < 0 {
		remainingHeight = 0
	}

	var mainContent string

	if remainingHeight > 0 {
		switch m.view {
		case viewList:
			mainContent = components.RenderDownloadList(m.list.downloads, m.list.selected, m.width, remainingHeight)
		case viewAdd:
			mainContent = m.renderAddView(remainingHeight)
		case viewConfirmRemove:
			mainContent = m.renderConfirmDialog("Are you sure you want to remove this download? (y/n)", remainingHeight)
		case viewConfirmCancel:
			mainContent = m.renderConfirmDialog("Are you sure you want to cancel this download? (y/n)", remainingHeight)
		}
	} else {
		mainContent = ""
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		notification,
		mainContent,
		footer,
	)
}

func (m *Model) renderAddView(height int) string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(styles.Pink).Render("Add New Download")
	b.WriteString(title)
	b.WriteString("\n\n" + m.urlInput.View())
	b.WriteString("\n\n" + m.priorityInput.View())

	err := m.priorityInput.Validate(m.priorityInput.Value())
	if err != nil {
		b.WriteString("\n" + styles.ErrorStyle.Render(err.Error()))
	}

	b.WriteString("\n\n(↑/↓ to switch, enter to confirm, esc to cancel)")

	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Pink).
		Padding(1, 2).
		Render(b.String())

	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) renderConfirmDialog(prompt string, height int) string {
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Red).
		Padding(1, 2).
		Render(prompt)

	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) renderNotification() string {
	if m.errMsg != "" {
		return styles.ErrorStyle.Width(m.width).Align(lipgloss.Center).Render(m.errMsg)
	}

	if m.successMsg != "" {
		return styles.SuccessStyle.Width(m.width).Align(lipgloss.Center).Render(m.successMsg)
	}

	return lipgloss.NewStyle().Height(1).Render("")
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
	queued := 0
	paused := 0
	completed := 0
	failed := 0

	for _, d := range m.list.downloads {
		switch d.Status {
		case 1:
			active++
		case 2:
			paused++
		case 3:
			completed++
		case 4:
			failed++
		case 5:
			queued++
		}
	}

	statsText := fmt.Sprintf(
		"Total: %d | Active: %d | Queued: %d | Paused: %d | completed: %d | Failed: %d",
		len(m.list.downloads), active, queued, paused, completed, failed,
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

func (m *Model) refreshDownloads() tea.Cmd {
	return func() tea.Msg {
		return downloadsMsg(m.actions.GetAll())
	}
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
			m.view = viewAdd
			m.urlInput.Focus()

			return textinput.Blink
		case key.Matches(msg, m.keys.Pause):
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.actions.Pause(id)
			}
		case key.Matches(msg, m.keys.Resume):
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.actions.Resume(id)
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

func (m *Model) updateAddView(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down):
			m.addFormFocusIndex = (m.addFormFocusIndex + 1) % 2
			if m.addFormFocusIndex == 0 {
				m.urlInput.Focus()
				m.priorityInput.Blur()
			} else {
				m.urlInput.Blur()
				m.priorityInput.Focus()
			}

		case key.Matches(msg, m.keys.Confirm):
			if m.urlInput.Value() != "" {
				priority := 5 // Default priority

				if m.priorityInput.Value() != "" {
					p, _ := strconv.Atoi(m.priorityInput.Value())
					priority = p
				}

				url := m.urlInput.Value()

				go func() {
					m.actions.Add(url, priority)
				}()

				m.successMsg = "Added download for: " + url
				m.view = viewList
				m.urlInput.SetValue("")
				m.priorityInput.SetValue("")
				m.addFormFocusIndex = 0

				return clearNotifications()
			}

		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			m.urlInput.SetValue("")
			m.priorityInput.SetValue("")
			m.addFormFocusIndex = 0
		}
	}

	var cmd tea.Cmd
	if m.addFormFocusIndex == 0 {
		m.urlInput, cmd = m.urlInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.priorityInput, cmd = m.priorityInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (m *Model) updateConfirmRemoveView(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			if id, ok := m.getSelectedDownloadID(); ok {
				go m.actions.Remove(id)
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
				go m.actions.Cancel(id)
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
