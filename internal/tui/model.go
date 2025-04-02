package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NamanBalaji/tdm/internal/common"
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
)

type messageModel struct {
	visible bool
	message string
	style   lipgloss.Style
	timer   *time.Timer
}

// Model represents the main TUI state
type Model struct {
	engine       *engine.Engine
	viewport     viewport.Model
	width        int
	height       int
	downloads    []*DownloadModel
	help         help.Model
	keys         keyMap
	activeView   view
	addDownload  AddDownloadModel
	spinner      spinner.Model
	messageModel messageModel
	errorMsg     string
	selectedIdx  int
	quitting     bool
	ready        bool
}

// NewModel creates a new TUI model
func NewModel(engine *engine.Engine) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(gruvboxGreen)

	help := help.New()
	help.ShowAll = false

	vp := viewport.New(80, 20)                            // Initial temporary size
	vp.Style = lipgloss.NewStyle().Background(gruvboxBg0) // Match background

	return Model{
		engine:     engine,
		help:       help,
		keys:       newKeyMap(),
		activeView: downloadListView,
		addDownload: AddDownloadModel{
			textInput: textinput.New(),
		},
		spinner:      s,
		viewport:     vp,
		messageModel: messageModel{style: lipgloss.NewStyle().Padding(1, 2).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(gruvboxAqua)},
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

		// Sort the downloads
		sortDownloadModels(models)

		return DownloadsLoadedMsg{Downloads: models}
	}
}

// sortDownloadModels sorts downloads with active ones at the top by latest added, then others by latest added
func sortDownloadModels(downloads []*DownloadModel) {
	// First separate active and non-active downloads
	var activeDownloads []*DownloadModel
	var otherDownloads []*DownloadModel

	for _, d := range downloads {
		stats := d.download.GetStats()
		if stats.Status == common.StatusActive {
			activeDownloads = append(activeDownloads, d)
		} else {
			otherDownloads = append(otherDownloads, d)
		}
	}

	// Sort active downloads by start time (descending)
	sort.Slice(activeDownloads, func(i, j int) bool {
		return activeDownloads[i].download.StartTime.After(activeDownloads[j].download.StartTime)
	})

	// Sort other downloads by start time (descending)
	sort.Slice(otherDownloads, func(i, j int) bool {
		return otherDownloads[i].download.StartTime.After(otherDownloads[j].download.StartTime)
	})

	// Clear the downloads slice and replace with sorted downloads
	if len(downloads) > 0 {
		// First, create a new slice with the correct sorted content
		sortedDownloads := make([]*DownloadModel, 0, len(downloads))
		sortedDownloads = append(sortedDownloads, activeDownloads...)
		sortedDownloads = append(sortedDownloads, otherDownloads...)

		// Then, copy the sorted content into the original slice
		copy(downloads, sortedDownloads)

		// If the original slice is larger than the sorted content, truncate it
		if len(sortedDownloads) < len(downloads) {
			for i := len(sortedDownloads); i < len(downloads); i++ {
				downloads[i] = nil
			}
			downloads = downloads[:len(sortedDownloads)]
		}
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
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate height for download list
		// Update download widths
		for _, d := range m.downloads {
			d.width = min(m.width-10, 90)
		}

		// Also update the add download view width
		m.addDownload.width = m.width - 10

		return m, nil

	case DownloadsLoadedMsg:
		m.downloads = msg.Downloads

		// Sort the downloads
		sortDownloadModels(m.downloads)

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

		// Re-sort after status updates
		sortDownloadModels(m.downloads)

		return m, nil

	case DownloadAddedMsg:
		newDownload := NewDownloadModel(msg.Download)
		newDownload.width = min(m.width-10, 90)
		m.downloads = append(m.downloads, newDownload)

		// Sort after adding new download
		sortDownloadModels(m.downloads)

		// Select the new download
		for i, d := range m.downloads {
			if d.download.ID == msg.Download.ID {
				m.selectedIdx = i
				break
			}
		}

		m.activeView = downloadListView
		m.errorMsg = ""
		m.showMessage(fmt.Sprintf("Download added: %s", msg.Download.Filename), gruvboxGreen)

		return m, nil

	case RemoveDownloadMsg:
		// Find and remove the download
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
		m.showMessage(fmt.Sprintf("Error: %s", msg.Error.Error()), gruvboxRed) // Use red color for errors

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
				// No need to scroll, we'll just render the next item selected
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if len(m.downloads) > 0 {
			prevIdx := m.selectedIdx
			m.selectedIdx = max(m.selectedIdx-1, 0)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				// No need to scroll, we'll just render the previous item selected
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		if len(m.downloads) > 0 {
			// Move down several items at once
			prevIdx := m.selectedIdx
			pageSize := 5 // Number of items to skip
			m.selectedIdx = min(m.selectedIdx+pageSize, len(m.downloads)-1)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				// No need to scroll, we'll just render the new selected item
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		if len(m.downloads) > 0 {
			// Move up several items at once
			prevIdx := m.selectedIdx
			pageSize := 5 // Number of items to skip
			m.selectedIdx = max(m.selectedIdx-pageSize, 0)

			// If selectedIdx changed, we need to update
			if prevIdx != m.selectedIdx {
				// No need to scroll, we'll just render the new selected item
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
			return m, cancelDownload(m.engine, download.download.ID)
		}

	case key.Matches(msg, m.keys.Remove):
		if len(m.downloads) > 0 && m.selectedIdx < len(m.downloads) {
			download := m.downloads[m.selectedIdx]
			return m, removeDownload(m.engine, download.download.ID)
		}
	}

	return m, nil
}

func (m Model) updateAddDownloadView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.activeView = downloadListView
		m.addDownload.textInput.Blur()
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

	var content string

	switch m.activeView {
	case downloadListView:
		content = m.renderDownloadListView()
	case addDownloadView:
		content = m.renderAddDownloadView()
	default:
		return "Unknown view"
	}

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}

func (m Model) renderDownloadListView() string {
	var s string

	contentWidth := min(m.width-10, 90)

	header := headerStyle.Width(contentWidth).Render("Terminal Download Manager (TDM)")
	s += header + "\n\n"

	if m.errorMsg != "" {
		s += errorStyle.Width(contentWidth).Render(m.errorMsg) + "\n\n"
	}

	// Render the downloads list
	if len(m.downloads) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(gruvboxFg2).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No downloads yet. Press 'a' to add a download.")
		s += empty + "\n\n"
	} else {
		// Build a scrollable region with the downloads
		var visibleContent strings.Builder

		// Get the number of visible items we can show
		availableHeight := m.height - 10 // Approximate height needed for header, message, and help
		itemHeight := 6                  // Approximate height of each download item
		visibleItems := max(1, availableHeight/itemHeight)

		// Calculate start and end indices for visible items
		startIdx := max(0, m.selectedIdx-(visibleItems/2))
		endIdx := min(len(m.downloads), startIdx+visibleItems)

		// Adjust startIdx if we don't have enough items to fill the view
		if endIdx-startIdx < visibleItems && startIdx > 0 {
			diff := visibleItems - (endIdx - startIdx)
			startIdx = max(0, startIdx-diff)
		}

		// Render only the visible downloads
		for i := startIdx; i < endIdx; i++ {
			download := m.downloads[i]
			if i == m.selectedIdx {
				visibleContent.WriteString(selectedDownloadStyle.Width(contentWidth).Render(download.View()) + "\n")
			} else {
				visibleContent.WriteString(downloadItemStyle.Width(contentWidth).Render(download.View()) + "\n")
			}
		}

		// Display scroll indicators if needed
		scrollIndicator := ""
		if startIdx > 0 {
			scrollIndicator += "↑ More above\n"
		}
		if endIdx < len(m.downloads) {
			scrollIndicator += "↓ More below\n"
		}

		if scrollIndicator != "" {
			scrollStyle := lipgloss.NewStyle().
				Foreground(gruvboxFg2).
				Align(lipgloss.Center).
				Width(contentWidth)
			s += scrollStyle.Render(scrollIndicator)
		}

		s += visibleContent.String()
	}

	if m.messageModel.visible {
		s += "\n" + m.messageModel.style.Render(m.messageModel.message)
	}

	helpView := m.help.View(m.keys)
	s += "\n" + helpView

	return s
}

func (m Model) renderAddDownloadView() string {
	var s string

	contentWidth := min(m.width-10, 90)

	header := headerStyle.Width(contentWidth).Render("Add New Download")
	s += header + "\n\n"

	m.addDownload.width = contentWidth
	s += m.addDownload.View() + "\n\n"

	helpView := m.help.View(m.keys)
	s += helpView

	return s
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
		// Sort downloads before updating
		sortDownloadModels(m.downloads)

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

func shutdownEngine(e *engine.Engine) tea.Cmd {
	return func() tea.Msg {
		e.Shutdown()
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
		err := e.CancelDownload(id, false)
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
