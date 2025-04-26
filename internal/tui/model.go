package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/tui/styles"
)

// Model is the main TUI application model.
type Model struct {
	engine        *engine.Engine
	stats         []downloader.Stats
	statsMap      map[string]int // Maps ID to index in stats slice to maintain order
	selected      int
	adding        bool
	input         textinput.Model
	confirmCancel bool
	confirmRemove bool
	spinner       spinner.Model
	help          help.Model
	keys          keyMap
	width, height int
	errMsg        string
	loaded        bool
	lastRefresh   time.Time // Track when we last refreshed
}

type (
	clearMsg       time.Time          // sent after delay to clear errMsg
	tickMsg        time.Time          // sent periodically to refresh stats
	statsUpdateMsg []downloader.Stats // stats update msg
)

func clearErrMsgAfter() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return clearMsg(t) })
}

// NewModel creates a new TUI model with the given engine.
func NewModel(e *engine.Engine) Model {
	// Text input for Add
	ti := textinput.New()
	ti.Placeholder = "Enter download URL"
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Line

	return Model{
		engine:      e,
		input:       ti,
		spinner:     sp,
		help:        help.New(),
		keys:        newKeyMap(),
		statsMap:    make(map[string]int),
		lastRefresh: time.Now().Add(-10 * time.Second), // Set to past time to ensure first refresh happens
	}
}

// Init initializes the model and starts periodic polling.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshCmd(),
		spinner.Tick,
		tea.Every(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

// refreshCmd fetches current download stats while preserving order.
func (m Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		dls := m.engine.ListDownloads()

		statsMap := make(map[string]int)
		for id, pos := range m.statsMap {
			statsMap[id] = pos
		}

		updatedStats := make([]downloader.Stats, len(m.stats))
		newEntries := []downloader.Stats{}

		for _, d := range dls {
			stats := d.GetStats()
			idStr := stats.ID.String()

			if idx, exists := statsMap[idStr]; exists && idx < len(updatedStats) {
				updatedStats[idx] = stats
			} else {
				newEntries = append(newEntries, stats)
			}
		}

		var filteredStats []downloader.Stats
		for _, s := range updatedStats {
			if s.ID != uuid.Nil {
				filteredStats = append(filteredStats, s)
			}
		}

		filteredStats = append(filteredStats, newEntries...)

		newStatsMap := make(map[string]int)
		for i, s := range filteredStats {
			newStatsMap[s.ID.String()] = i
		}

		return statsUpdateMsg(filteredStats)
	}
}

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.adding {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			switch msg.String() {
			case "enter":
				url := m.input.Value()
				m.adding = false
				m.input.SetValue("")
				if url != "" {
					id, err := m.engine.AddDownload(url, nil)
					if err != nil {
						m.errMsg = err.Error()
					} else {
						dl, _ := m.engine.GetDownload(id)
						m.errMsg = fmt.Sprintf("Download %s added", dl.Filename)
					}
					return m, tea.Batch(m.refreshCmd(), clearErrMsgAfter())
				}
				return m, nil
			case "esc":
				m.adding = false
				m.input.SetValue("")
				return m, nil
			}
			return m, cmd
		}

		if m.confirmCancel {
			switch msg.String() {
			case "y", "enter":
				if len(m.stats) > 0 && m.selected < len(m.stats) {
					sel := m.stats[m.selected]
					m.engine.CancelDownload(sel.ID, false)
				}
				m.confirmCancel = false
				return m, m.refreshCmd()
			case "n", "esc":
				m.confirmCancel = false
				return m, nil
			}
			return m, nil
		}

		if m.confirmRemove {
			switch msg.String() {
			case "y", "enter":
				if len(m.stats) > 0 && m.selected < len(m.stats) {
					sel := m.stats[m.selected]
					m.engine.RemoveDownload(sel.ID, true)
				}
				m.confirmRemove = false
				return m, m.refreshCmd()
			case "n", "esc":
				m.confirmRemove = false
				return m, nil
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Add):
			m.adding = true
			return m, nil
		case key.Matches(msg, m.keys.Pause):
			if len(m.stats) == 0 || m.selected >= len(m.stats) {
				return m, nil
			}
			sel := m.stats[m.selected]
			m.engine.PauseDownload(sel.ID)
			return m, m.refreshCmd()
		case key.Matches(msg, m.keys.Resume):
			if len(m.stats) == 0 || m.selected >= len(m.stats) {
				return m, nil
			}
			sel := m.stats[m.selected]
			err := m.engine.ResumeDownload(sel.ID)
			if err != nil {
				m.errMsg = fmt.Sprintf("Failed to resume download: %v", err)
				return m, clearErrMsgAfter()
			}
			return m, m.refreshCmd()
		case key.Matches(msg, m.keys.Cancel):
			if len(m.stats) == 0 || m.selected >= len(m.stats) {
				return m, nil
			}
			m.confirmCancel = true
			return m, nil
		case key.Matches(msg, m.keys.Remove):
			if len(m.stats) == 0 || m.selected >= len(m.stats) {
				return m, nil
			}
			m.confirmRemove = true
			return m, nil
		case key.Matches(msg, m.keys.Up):
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if m.selected < len(m.stats)-1 {
				m.selected++
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = m.width/2 - 4
		return m, m.refreshCmd()
	case tickMsg:
		now := time.Now()
		if now.Sub(m.lastRefresh) < 500*time.Millisecond {
			return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return tickMsg(t)
			})
		}

		newModel := m
		newModel.lastRefresh = now
		return newModel, m.refreshCmd()

	case clearMsg:
		newModel := m
		newModel.errMsg = ""
		return newModel, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		newModel := m
		newModel.spinner, cmd = m.spinner.Update(msg)
		return newModel, cmd

	case statsUpdateMsg:
		newModel := m
		newModel.stats = msg

		newModel.statsMap = make(map[string]int)
		for i, s := range newModel.stats {
			newModel.statsMap[s.ID.String()] = i
		}

		newModel.loaded = true
		if newModel.selected >= len(newModel.stats) {
			newModel.selected = len(newModel.stats) - 1
			if newModel.selected < 0 {
				newModel.selected = 0
			}
		}

		hasActiveDownloads := false
		for _, stat := range newModel.stats {
			if stat.Status == common.StatusActive {
				hasActiveDownloads = true
				break
			}
		}

		nextUpdateDelay := 1000 * time.Millisecond
		if hasActiveDownloads {
			nextUpdateDelay = 500 * time.Millisecond
		}

		return newModel, tea.Tick(nextUpdateDelay, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

// View renders the complete UI with the updated layout.
func (m Model) View() string {
	if m.adding {
		m.input.Width = 40

		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Pink).
			Render("Add Download")

		modal := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Width(50).
			Render(title + "\n\n" + m.input.View())

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	if m.confirmCancel {
		msg := "Cancel this download? (y/n)"
		dialog := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).
			Width(lipgloss.Width(msg) + 4).Align(lipgloss.Center).
			Render(msg)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
	}

	if m.confirmRemove {
		msg := "Remove this download? (y/n)"
		dialog := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).
			Width(lipgloss.Width(msg) + 4).Align(lipgloss.Center).
			Render(msg)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
	}

	if m.loaded && len(m.stats) == 0 {
		logo := []string{
			"████████╗██████╗ ███╗   ███╗",
			"╚══██╔══╝██╔══██╗████╗ ████║",
			"   ██║   ██║  ██║██╔████╔██║",
			"   ██║   ██║  ██║██║╚██╔╝██║",
			"   ██║   ██████╔╝██║ ╚═╝ ██║",
			"   ╚═╝   ╚═════╝ ╚═╝     ╚═╝",
		}
		colors := []lipgloss.Color{
			styles.Blue, styles.Mauve, styles.Red,
			styles.Peach, styles.Yellow, styles.Green,
		}
		var lines []string
		for i, line := range logo {
			styled := lipgloss.NewStyle().Foreground(colors[i]).Align(lipgloss.Center).Render(line)
			lines = append(lines, styled)
		}
		subtitle := lipgloss.NewStyle().Foreground(styles.Text).Italic(true).Align(lipgloss.Center).Render("Terminal Download Manager")

		instruction := lipgloss.NewStyle().Foreground(styles.Subtext0).Align(lipgloss.Center).Render("Press 'a' to add a download or 'q' to quit")
		content := lipgloss.JoinVertical(lipgloss.Center, lines...)
		content = lipgloss.JoinVertical(lipgloss.Center, content, "", subtitle, "", instruction)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	headerContent := renderHeader(m)

	var errorBox string
	if m.errMsg != "" {
		errorBox = renderErrorBox(m)
	}

	padding := lipgloss.NewStyle().Height(1).Render("")

	listHeight := m.height - 10
	if listHeight < 10 {
		listHeight = 10
	}

	bodyStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Width(m.width)

	body := bodyStyle.Render(RenderDownloadList(&m, m.width-4, listHeight))

	// Footer with help
	footerStyle := styles.FooterStyle.
		Width(m.width).
		Padding(1, 2).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(styles.Surface0)

	footer := footerStyle.Render(m.help.View(m.keys))

	parts := []string{headerContent}

	if errorBox != "" {
		parts = append(parts, errorBox)
	} else {
		parts = append(parts, padding)
	}

	parts = append(parts, padding, body, footer)

	return lipgloss.JoinVertical(lipgloss.Top, parts...)
}

// countStatus returns how many downloads match the given status.
func (m Model) countStatus(status common.Status) int {
	count := 0
	for _, st := range m.stats {
		if st.Status == status {
			count++
		}
	}
	return count
}

// renderHeader creates a nice header with logo and stats.
func renderHeader(m Model) string {
	title := "TDM - Terminal Download Manager"
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Crust).
		Background(styles.Pink).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	header := headerStyle.Render(title)

	total := len(m.stats)
	active := m.countStatus(common.StatusActive)
	paused := m.countStatus(common.StatusPaused)
	failed := m.countStatus(common.StatusFailed)
	completed := m.countStatus(common.StatusCompleted)

	statsText := fmt.Sprintf(
		"Total: %d   |   Active: %d   |   Paused: %d   |   Completed: %d   |   Failed: %d",
		total, active, paused, completed, failed,
	)

	statsStyle := lipgloss.NewStyle().
		Foreground(styles.Text).
		Background(styles.Surface0).
		Padding(0, 2).
		Width(m.width).
		Align(lipgloss.Center)

	stats := statsStyle.Render(statsText)

	return lipgloss.JoinVertical(lipgloss.Top, header, stats)
}

// renderErrorBox renders the error/notification box centered under the stats.
func renderErrorBox(m Model) string {
	msgWidth := lipgloss.Width(m.errMsg) + 8
	maxWidth := m.width - 4
	if msgWidth > maxWidth {
		msgWidth = maxWidth
	}

	var style lipgloss.Style
	if strings.HasPrefix(m.errMsg, "Download") {
		style = styles.SuccessStyle.
			Width(msgWidth).
			Padding(0, 2).
			Align(lipgloss.Center)
	} else {
		style = styles.ErrorStyle.
			Width(msgWidth).
			Padding(0, 2).
			Align(lipgloss.Center)
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Center).
		Padding(1, 0).
		Render(style.Render(m.errMsg))
}
