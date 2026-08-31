package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/tui/components"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

type currentView int

type confirmAction int

var (
	errPriorityNAN   = errors.New("priority must be a number")
	errPriorityRange = errors.New("priority must be between 1 and 10")
)

const (
	viewList currentView = iota
	viewAdd
	viewConfirm
)

const (
	confirmRemove confirmAction = iota
	confirmCancel
)

// Model is the main TUI application model.
type Model struct {
	actions           managerActions
	view              currentView
	pendingConfirm    confirmAction
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
	loaded        bool
}

type listModel struct {
	downloads []download.DownloadInfo
	selected  int
}

type (
	clearMsg      struct{}
	tickMsg       struct{}
	downloadsMsg  []download.DownloadInfo
	downloadError struct{ error }
)

func clearNotifications() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearMsg{}
	})
}

// NewModel creates a new TUI model.
func NewModel(actions managerActions) *Model {
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
			return errPriorityNAN
		}

		if p < 1 || p > 10 {
			return errPriorityRange
		}

		return nil
	}

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(styles.Pink)

	return &Model{
		actions:       actions,
		view:          viewList,
		urlInput:      urlInput,
		priorityInput: priorityInput,
		spinner:       sp,
		help:          help.New(),
		keys:          newKeyMap(),
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshDownloads(),
		m.spinner.Tick,
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg{}
		}),
	)
}

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width

	case tickMsg:
		return m, tea.Batch(
			m.refreshDownloads(),
			tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} }),
		)

	case downloadsMsg:
		m.list.downloads = msg
		if !m.loaded {
			m.loaded = true
		}

		m.list.selected = max(min(m.list.selected, len(m.list.downloads)-1), 0)

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
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
	}

	switch m.view {
	case viewList:
		cmd = m.updateListView(msg)
	case viewAdd:
		cmd = m.updateAddView(msg)
	case viewConfirm:
		cmd = m.updateConfirmView(msg)
	}

	return m, cmd
}

// View renders the TUI.
func (m *Model) View() string {
	if !m.loaded {
		return fmt.Sprintf("\n  %s Loading downloads... Please wait.\n\n", m.spinner.View())
	}

	header := renderHeader(m)
	footer := styles.FooterStyle.Width(m.width).Render(m.help.View(m.keys))
	notification := m.renderNotification()

	remainingHeight := max(m.height-lipgloss.Height(header)-lipgloss.Height(notification)-lipgloss.Height(footer), 0)

	var mainContent string

	if remainingHeight > 0 {
		switch m.view {
		case viewList:
			mainContent = components.RenderDownloadList(m.list.downloads, m.list.selected, m.width, remainingHeight)
		case viewAdd:
			mainContent = m.renderAddView(remainingHeight)
		case viewConfirm:
			mainContent = m.renderConfirmDialog(m.confirmPrompt(), remainingHeight)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		notification,
		mainContent,
		footer,
	)
}

func (m *Model) confirmPrompt() string {
	switch m.pendingConfirm {
	case confirmRemove:
		return "Are you sure you want to remove this download? (y/n)"
	case confirmCancel:
		return "Are you sure you want to cancel this download? (y/n)"
	default:
		return ""
	}
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
		case download.Active:
			active++
		case download.Paused:
			paused++
		case download.Completed:
			completed++
		case download.Failed:
			failed++
		case download.Queued:
			queued++
		case download.Pending, download.Cancelled:
		}
	}

	statsText := fmt.Sprintf(
		"Total: %d | Active: %d | Queued: %d | Paused: %d | Completed: %d | Failed: %d",
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

	return uuid.Nil(), false
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
				return func() tea.Msg {
					m.actions.Pause(id)
					return nil
				}
			}
		case key.Matches(msg, m.keys.Resume):
			if id, ok := m.getSelectedDownloadID(); ok {
				return func() tea.Msg {
					m.actions.Resume(id)
					return nil
				}
			}
		case key.Matches(msg, m.keys.Cancel):
			if _, ok := m.getSelectedDownloadID(); ok {
				m.pendingConfirm = confirmCancel
				m.view = viewConfirm
			}
		case key.Matches(msg, m.keys.Remove):
			if _, ok := m.getSelectedDownloadID(); ok {
				m.pendingConfirm = confirmRemove
				m.view = viewConfirm
			}
		}
	}

	return nil
}

func (m *Model) updateAddView(msg tea.Msg) tea.Cmd {
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
				priority := 5

				if m.priorityInput.Value() != "" {
					p, _ := strconv.Atoi(m.priorityInput.Value())
					priority = p
				}

				url := m.urlInput.Value()
				m.successMsg = "Added download for: " + url
				m.view = viewList
				m.urlInput.SetValue("")
				m.priorityInput.SetValue("")
				m.addFormFocusIndex = 0

				return tea.Batch(
					func() tea.Msg {
						if err := m.actions.Add(url, priority); err != nil {
							return downloadError{err}
						}

						return nil
					},
					clearNotifications(),
				)
			}

		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			m.urlInput.SetValue("")
			m.priorityInput.SetValue("")
			m.addFormFocusIndex = 0

			return nil
		}
	}

	var cmd tea.Cmd
	if m.addFormFocusIndex == 0 {
		m.urlInput, cmd = m.urlInput.Update(msg)
	} else {
		m.priorityInput, cmd = m.priorityInput.Update(msg)
	}

	return cmd
}

func (m *Model) updateConfirmView(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			if id, ok := m.getSelectedDownloadID(); ok {
				action := m.pendingConfirm
				m.view = viewList

				return func() tea.Msg {
					switch action {
					case confirmRemove:
						m.actions.Remove(id)
					case confirmCancel:
						m.actions.Cancel(id)
					}

					return nil
				}
			}

			m.view = viewList
		case "n", "N", "esc":
			m.view = viewList
		}
	}

	return nil
}
