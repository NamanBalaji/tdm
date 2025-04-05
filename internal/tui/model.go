package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// view represents the different screens in the TUI
type view int

const (
	downloadListView view = iota
	addDownloadView
	confirmCancelView
	confirmRemoveView
)

type messageModel struct {
	visible bool
	message string
	style   lipgloss.Style
	timer   *time.Timer
}

// Model represents the main TUI state
type Model struct {
	engine        *engine.Engine
	viewport      viewport.Model
	width         int
	height        int
	downloads     []*DownloadModel
	help          help.Model
	keys          keyMap
	activeView    view
	addDownload   AddDownloadModel
	spinner       spinner.Model
	messageModel  messageModel
	errorMsg      string
	selectedIdx   int
	quitting      bool
	ready         bool
	confirmDialog ConfirmDialogModel
}

// NewModel creates a new TUI model
func NewModel(engine *engine.Engine) Model {
	s := spinner.New()
	s.Spinner = spinner.Hamburger
	s.Style = lipgloss.NewStyle().Foreground(catpBlue)

	help := help.New()
	help.ShowAll = false

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().Background(catpBase)

	return Model{
		engine:     engine,
		help:       help,
		keys:       newKeyMap(),
		activeView: downloadListView,
		addDownload: AddDownloadModel{
			textInput: textinput.New(),
		},
		spinner:  s,
		viewport: vp,
		messageModel: messageModel{
			style: lipgloss.NewStyle().
				Padding(1, 2).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(catpLavender).
				Width(60). // Set a fixed width for the modal
				Align(lipgloss.Center). // Center the content horizontally
				MaxWidth(80). // Maximum width to prevent overly wide modals
				MaxHeight(20), // Maximum height to prevent overly tall modals
		},
	}
}

// Init initializes the TUI model
func (m Model) Init() tea.Cmd {
	m.addDownload.textInput.Placeholder = "Enter URL to download"
	m.addDownload.textInput.Focus()
	m.addDownload.textInput.Width = 60
	m.ready = false

	return tea.Batch(
		m.loadDownloads(),
		spinner.Tick,
		tea.EnterAltScreen,
	)
}

// loadDownloads loads existing downloads from the engine
func (m Model) loadDownloads() tea.Cmd {
	return func() tea.Msg {
		downloads := m.engine.ListDownloads()
		var models []*DownloadModel

		for _, d := range downloads {
			models = append(models, NewDownloadModel(d))
		}

		sort.Slice(models, func(i, j int) bool {
			return models[j].download.StartTime.After(models[i].download.StartTime)
		})

		return DownloadsLoadedMsg{Downloads: models}
	}
}

// Update handles input and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, shutdownEngine(m.engine)

		case m.activeView == downloadListView:
			return m.updateDownloadListView(msg)

		case m.activeView == addDownloadView:
			return m.updateAddDownloadView(msg)

		case m.activeView == confirmCancelView:
			return m.updateConfirmCancelView(msg)

		case m.activeView == confirmRemoveView:
			return m.updateConfirmRemoveView(msg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		contentWidth := min(msg.Width-4, 90)

		for _, d := range m.downloads {
			d.width = contentWidth
		}

		m.addDownload.width = contentWidth

		if m.activeView == confirmCancelView || m.activeView == confirmRemoveView {
			m.confirmDialog.width = contentWidth - 20
		}

		if len(m.downloads) > 0 {
			if m.selectedIdx >= len(m.downloads) {
				m.selectedIdx = len(m.downloads) - 1
			}
		}

		return m, tea.ClearScreen

	case DownloadsLoadedMsg:
		m.downloads = msg.Downloads

		for _, d := range m.downloads {
			d.width = min(m.width-10, 90)
		}

		return m, nil

	case StatusUpdateMsg:
		for i, d := range m.downloads {
			if d.download.ID == msg.ID {
				m.downloads[i].Update(msg)
				break
			}
		}

		return m, nil

	case DownloadAddedMsg:
		newDownload := NewDownloadModel(msg.Download)
		newDownload.width = min(m.width-10, 90)

		m.downloads = append(m.downloads, newDownload)
		m.selectedIdx = 0

		m.activeView = downloadListView
		m.errorMsg = ""
		m.showMessage(fmt.Sprintf("Download added: %s", msg.Download.Filename), catpGreen)

		return m, nil

	case RemoveDownloadMsg:
		for i, d := range m.downloads {
			if d.download.ID == msg.ID {
				m.downloads = append(m.downloads[:i], m.downloads[i+1:]...)
				if m.selectedIdx >= len(m.downloads) {
					m.selectedIdx = max(0, len(m.downloads)-1)
				}
				break
			}
		}

		return m, nil

	case ErrorMsg:
		m.showMessage(fmt.Sprintf("Error: %s", msg.Error.Error()), catpRed)

		if m.activeView == addDownloadView {
			m.activeView = downloadListView
		}
		return m, nil

	case MessageTimeoutMsg:
		m.messageModel.visible = false
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		for _, d := range m.downloads {
			if d.download.Status == "active" {
				d.spinner, _ = d.spinner.Update(msg)
			}
		}

		cmds = append(cmds, cmd, m.updateDownloadStatuses())
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m Model) updateDownloadListView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Add):
		m.activeView = addDownloadView
		m.addDownload.textInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if len(m.downloads) > 0 {
			prevIdx := m.selectedIdx
			m.selectedIdx = min(m.selectedIdx+1, len(m.downloads)-1)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if len(m.downloads) > 0 {
			prevIdx := m.selectedIdx
			m.selectedIdx = max(m.selectedIdx-1, 0)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		if len(m.downloads) > 0 {
			prevIdx := m.selectedIdx
			pageSize := 5
			m.selectedIdx = min(m.selectedIdx+pageSize, len(m.downloads)-1)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		if len(m.downloads) > 0 {
			prevIdx := m.selectedIdx
			pageSize := 5
			m.selectedIdx = max(m.selectedIdx-pageSize, 0)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Pause):
		if len(m.downloads) > 0 && m.selectedIdx < len(m.downloads) {
			download := m.downloads[m.selectedIdx]
			return m, pauseDownload(m.engine, download.download.ID)
		}

	case key.Matches(msg, m.keys.Resume):
		if len(m.downloads) > 0 && m.selectedIdx < len(m.downloads) {
			download := m.downloads[m.selectedIdx]
			return m, resumeDownload(m.engine, download.download.ID)
		}

	case key.Matches(msg, m.keys.Cancel):
		if len(m.downloads) > 0 && m.selectedIdx < len(m.downloads) {
			download := m.downloads[m.selectedIdx]

			m.confirmDialog = ConfirmDialogModel{
				title:    "Cancel Download",
				message:  fmt.Sprintf("Are you sure you want to cancel '%s'?", download.download.Filename),
				action:   "cancel",
				targetID: download.download.ID,
				width:    min(m.width-20, 60),
			}
			m.activeView = confirmCancelView
			return m, nil
		}

	case key.Matches(msg, m.keys.Remove):
		if len(m.downloads) > 0 && m.selectedIdx < len(m.downloads) {
			download := m.downloads[m.selectedIdx]

			m.confirmDialog = ConfirmDialogModel{
				title:    "Remove Download",
				message:  fmt.Sprintf("Are you sure you want to remove '%s'?", download.download.Filename),
				action:   "remove",
				targetID: download.download.ID,
				width:    min(m.width-20, 60),
			}
			m.activeView = confirmRemoveView
			return m, nil
		}
	}

	return m, nil
}

func (m Model) updateAddDownloadView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.activeView = downloadListView
		m.addDownload.textInput.Blur()
		m.addDownload.textInput.SetValue("")
		return m, nil

	case key.Matches(msg, m.keys.Confirm):
		url := m.addDownload.textInput.Value()
		if url != "" {
			m.addDownload.textInput.SetValue("")
			return m, addDownload(m.engine, url)
		}
		m.activeView = downloadListView
		m.addDownload.textInput.Blur()
		m.addDownload.textInput.SetValue("")
		return m, nil

	default:
		var cmd tea.Cmd
		m.addDownload.textInput, cmd = m.addDownload.textInput.Update(msg)
		return m, cmd
	}
}

// View renders the TUI
func (m Model) View() string {
	if m.quitting {
		return "Shutting down TDM...\n"
	}
	termWidth := m.width
	termHeight := m.height

	contentWidth := termWidth - 4
	if contentWidth > 90 {
		contentWidth = 90
	}
	if contentWidth < 40 {
		contentWidth = 40
	}

	var content string
	switch m.activeView {
	case downloadListView:
		content = m.renderDownloadListView(contentWidth)
	case addDownloadView:
		content = m.renderAddDownloadView(contentWidth)
	case confirmCancelView, confirmRemoveView:
		content = m.confirmDialog.View()
	default:
		return "Unknown view"
	}

	return lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) renderDownloadListView(contentWidth int) string {
	var s strings.Builder

	// Update all styles with current width
	header := headerStyle.Width(contentWidth).Render("Terminal Download Manager (TDM)")
	s.WriteString(header)
	s.WriteString("\n\n")

	if m.errorMsg != "" {
		errorText := errorStyle.Width(contentWidth).Render(m.errorMsg)
		s.WriteString(errorText)
		s.WriteString("\n\n")
	}

	// Render the downloads list
	if len(m.downloads) == 0 {
		s.Reset()

		logoLines := []string{
			"████████╗██████╗ ███╗   ███╗",
			"╚══██╔══╝██╔══██╗████╗ ████║",
			"   ██║   ██║  ██║██╔████╔██║",
			"   ██║   ██║  ██║██║╚██╔╝██║",
			"   ██║   ██████╔╝██║ ╚═╝ ██║",
			"   ╚═╝   ╚═════╝ ╚═╝     ╚═╝",
		}

		// Apply Catppuccin gradient colors to the logo
		colors := []lipgloss.Color{catpBlue, catpMauve, catpRed, catpPeach, catpYellow, catpGreen}

		var coloredLogo strings.Builder
		for i, line := range logoLines {
			coloredLine := lipgloss.NewStyle().
				Foreground(colors[i]).
				Align(lipgloss.Center).
				Bold(true).
				Width(contentWidth).
				Render(line)
			coloredLogo.WriteString(coloredLine + "\n")
		}

		// Add a subtitle
		subtitle := lipgloss.NewStyle().
			Foreground(catpText).
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("  Terminal Download Manager")

		// Add instruction
		instruction := lipgloss.NewStyle().
			Foreground(catpSubtext0).
			Align(lipgloss.Center).
			Width(contentWidth).
			Margin(1, 0).
			Render("   Press 'a' to add a download")

		// Combine all elements
		s.WriteString(coloredLogo.String())
		s.WriteString("\n")
		s.WriteString(subtitle)
		s.WriteString("\n\n")
		s.WriteString(instruction)

		s.WriteString("\n\n")
	} else {
		// Calculate how many items we can show based on available space
		availableHeight := m.height - 10 // Space for header, footer, etc.
		itemHeight := 6                  // Approximate height of each download item
		visibleItems := max(1, availableHeight/itemHeight)

		// Calculate which portion of downloads to show
		startIdx := max(0, m.selectedIdx-(visibleItems/2))
		endIdx := min(len(m.downloads), startIdx+visibleItems)

		// Adjust startIdx if we don't have enough items to fill the view
		if endIdx-startIdx < visibleItems && startIdx > 0 {
			diff := visibleItems - (endIdx - startIdx)
			startIdx = max(0, startIdx-diff)
			endIdx = min(len(m.downloads), startIdx+visibleItems)
		}

		if startIdx > 0 {
			scrollUpIndicator := lipgloss.NewStyle().
				Foreground(catpSubtext0).
				Align(lipgloss.Center).
				Width(contentWidth).
				Render("↑ More above")
			s.WriteString(scrollUpIndicator + "\n")
		}

		// Update all download widths to match current content width
		for _, download := range m.downloads {
			download.width = contentWidth
		}

		// Render visible downloads with consistent styling
		for i := startIdx; i < endIdx; i++ {
			download := m.downloads[i]
			var downloadView string

			if i == m.selectedIdx {
				downloadView = selectedDownloadStyle.
					Width(contentWidth).
					Render(download.View())
			} else {
				downloadView = downloadItemStyle.
					Width(contentWidth).
					Render(download.View())
			}

			s.WriteString(downloadView + "\n")
		}

		// Show bottom scroll indicator if needed
		if endIdx < len(m.downloads) {
			scrollDownIndicator := lipgloss.NewStyle().
				Foreground(catpSubtext0).
				Align(lipgloss.Center).
				Width(contentWidth).
				Render("↓ More below")
			s.WriteString(scrollDownIndicator + "\n")
		}
	}

	// Add message if visible
	if m.messageModel.visible {
		s.WriteString("\n")
		messageStyle := m.messageModel.style
		s.WriteString(messageStyle.Render(m.messageModel.message))
	}

	// Add help view
	helpView := m.help.View(m.keys)
	s.WriteString("\n")
	s.WriteString(helpView)

	return s.String()
}

func (m Model) renderAddDownloadView(contentWidth int) string {
	var s strings.Builder

	// Update the add download view width
	m.addDownload.width = contentWidth

	// Create the header with proper width
	header := headerStyle.Copy().Width(contentWidth).Render("Add New Download")
	s.WriteString(header)
	s.WriteString("\n\n")

	// Render the form with the current width
	formView := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(m.addDownload.View())
	s.WriteString(formView)
	s.WriteString("\n\n")

	// Add help view
	helpView := m.help.View(m.keys)
	s.WriteString(helpView)

	return s.String()
}

func (m *Model) showMessage(msg string, color lipgloss.Color) {
	m.messageModel.message = msg
	m.messageModel.visible = true
	m.messageModel.style = m.messageModel.style.BorderForeground(color)

	if m.messageModel.timer != nil {
		m.messageModel.timer.Stop()
	}

	m.messageModel.timer = time.AfterFunc(3*time.Second, func() {
		program.Send(MessageTimeoutMsg{})
	})
}

func (m Model) updateDownloadStatuses() tea.Cmd {
	return func() tea.Msg {
		for _, d := range m.downloads {
			stats := d.download.GetStats()
			d.Update(StatusUpdateMsg{
				ID:       d.download.ID,
				Progress: stats.Progress,
				Speed:    stats.Speed,
				Status:   stats.Status,
			})
		}
		return nil
	}
}

func (m Model) updateConfirmCancelView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit):
		// User cancelled the operation
		m.activeView = downloadListView
		return m, nil

	case key.Matches(msg, m.keys.Confirm):
		// User confirmed the cancellation
		m.activeView = downloadListView
		return m, cancelDownload(m.engine, m.confirmDialog.targetID)
	}

	return m, nil
}

func (m Model) updateConfirmRemoveView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit):
		// User cancelled the operation
		m.activeView = downloadListView
		return m, nil

	case key.Matches(msg, m.keys.Confirm):
		// User confirmed the removal
		m.activeView = downloadListView
		return m, removeDownload(m.engine, m.confirmDialog.targetID)
	}

	return m, nil
}

func shutdownEngine(e *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		if err := e.Shutdown(); err != nil {
			logger.Errorf("error shutting down engine: %v", err)
		}
		return tea.Quit()
	}
}

func addDownload(e *engine.Engine, url string) tea.Cmd {
	return func() tea.Msg {
		id, err := e.AddDownload(url, nil)
		if err != nil {
			return ErrorMsg{Error: err}
		}

		download, err := e.GetDownload(id)
		if err != nil {
			return ErrorMsg{Error: err}
		}

		return DownloadAddedMsg{Download: download}
	}
}

func pauseDownload(e *engine.Engine, id uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		err := e.PauseDownload(id)
		if err != nil {
			return ErrorMsg{Error: err}
		}
		return nil
	}
}

func resumeDownload(e *engine.Engine, id uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		err := e.ResumeDownload(context.Background(), id)
		if err != nil {
			return ErrorMsg{Error: err}
		}
		return nil
	}
}

func cancelDownload(e *engine.Engine, id uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		err := e.CancelDownload(id, true)
		if err != nil {
			return ErrorMsg{Error: err}
		}
		return nil
	}
}

func removeDownload(e *engine.Engine, id uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		err := e.RemoveDownload(id, true)
		if err != nil {
			return ErrorMsg{Error: err}
		}
		return RemoveDownloadMsg{ID: id}
	}
}
