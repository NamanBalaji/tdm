package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/engine"
)

// Run initializes and starts the TUI.
func Run(ctx context.Context, eng *engine.Engine) error {
	m := NewModel(newEngineActions(ctx, eng))
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()

	return err
}

type engineActions struct {
	Pause  func(id uuid.UUID)
	Resume func(id uuid.UUID)
	Add    func(url string, prio int)
	Cancel func(id uuid.UUID)
	Remove func(id uuid.UUID)
	GetAll func() []engine.DownloadInfo
}

func newEngineActions(ctx context.Context, e *engine.Engine) engineActions {
	return engineActions{
		Pause:  func(id uuid.UUID) { e.PauseDownload(ctx, id) },
		Resume: func(id uuid.UUID) { e.ResumeDownload(ctx, id) },
		Add:    func(url string, p int) { e.AddDownload(ctx, url, p) },
		Cancel: func(id uuid.UUID) { e.CancelDownload(ctx, id) },
		Remove: func(id uuid.UUID) { e.RemoveDownload(ctx, id) },
		GetAll: e.GetAllDownloads,
	}
}
