package tui

import (
	"context"

	"github.com/google/uuid"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/manager"
)

// Run initializes and starts the TUI.
func Run(ctx context.Context, mgr *manager.Manager) error {
	m := NewModel(newManagerActions(ctx, mgr))
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-mgr.GetErrors():
				if !ok {
					return
				}

				p.Send(downloadError{err.Error})
			}
		}
	}()

	_, err := p.Run()

	return err
}

type managerActions struct {
	Pause  func(id uuid.UUID)
	Resume func(id uuid.UUID)
	Add    func(url string, prio int) error
	Cancel func(id uuid.UUID)
	Remove func(id uuid.UUID)
	GetAll func() []download.DownloadInfo
}

func newManagerActions(ctx context.Context, m *manager.Manager) managerActions {
	return managerActions{
		Pause:  func(id uuid.UUID) { m.PauseDownload(ctx, id) },
		Resume: func(id uuid.UUID) { m.ResumeDownload(ctx, id) },
		Add:    func(url string, p int) error { _, err := m.AddDownload(ctx, url, p); return err },
		Cancel: func(id uuid.UUID) { m.CancelDownload(ctx, id) },
		Remove: func(id uuid.UUID) { m.RemoveDownload(ctx, id) },
		GetAll: m.GetAllDownloads,
	}
}
