package tui

import (
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

var program *tea.Program

// Message types for the TUI
type (
	// DownloadsLoadedMsg is sent when downloads are loaded
	DownloadsLoadedMsg struct {
		Downloads []*DownloadModel
	}

	// StatusUpdateMsg is sent when a download status changes
	StatusUpdateMsg struct {
		ID       uuid.UUID
		Progress float64
		Speed    int64
		Status   common.Status
		Error    string
	}

	// DownloadAddedMsg is sent when a new download is added
	DownloadAddedMsg struct {
		Download *downloader.Download
	}

	// RemoveDownloadMsg is sent when a download is removed
	RemoveDownloadMsg struct {
		ID uuid.UUID
	}

	// ErrorMsg is sent when an error occurs
	ErrorMsg struct {
		Error error
	}

	// MessageTimeoutMsg is sent when a notification should be hidden
	MessageTimeoutMsg struct{}
)

// Run starts the TUI application
func Run(engine *engine.Engine) error {
	model := NewModel(engine)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	program = p

	_, err := p.Run()
	return err
}
