package worker

import (
	"context"
	"errors"
	"net/url"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
)

var ErrUnsupportedScheme = errors.New("unsupported scheme")

// Worker defines the interface for a worker that can perform downloads.
type Worker interface {
	Start(ctx context.Context) error
	Pause() error
	Cancel() error
	Resume(ctx context.Context) error
	Remove() error
	Done() <-chan error
	Progress() progress.Progress
	GetPriority() int
	GetID() uuid.UUID
	GetStatus() status.Status
	GetFilename() string
	Queue()
}

func GetWorker(ctx context.Context, urlStr string, repo *repository.BboltRepository) (Worker, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		if http.CanHandle(urlStr) {
			return http.New(ctx, urlStr, nil, repo)
		}
	}

	return nil, ErrUnsupportedScheme
}
