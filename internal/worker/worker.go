package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/progress"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/torrent"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

var ErrUnsupportedScheme = errors.New("unsupported scheme")

// Worker defines the interface for a worker that can perform downloads.
type Worker interface {
	Start(ctx context.Context) error
	Pause() error
	Cancel() error
	Remove() error
	Done() <-chan error
	Progress() progress.Progress
	GetPriority() int
	GetID() uuid.UUID
	GetStatus() status.Status
	GetFilename() string
	Queue()
}

// GetWorker returns a worker based on the URL scheme.
func GetWorker(ctx context.Context, cfg *config.Config, urlStr string, priority int, torrentClient *torrentPkg.Client, repo *repository.BboltRepository) (Worker, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		if torrentPkg.HasTorrentFile(urlStr) {
			return torrent.New(ctx, nil, urlStr, false, cfg.Torrent.DownloadDir, torrentClient, repo, priority)
		}

		if http.CanHandle(urlStr) {
			return http.New(ctx, cfg.Http, urlStr, nil, repo, priority)
		}
	case "magnet":
		if torrentPkg.IsValidMagnetLink(urlStr) {
			return torrent.New(ctx, nil, urlStr, true, cfg.Torrent.DownloadDir, torrentClient, repo, priority)
		}
	}

	return nil, ErrUnsupportedScheme
}

// LoadWorker loads a worker from a saved download object.
func LoadWorker(ctx context.Context, cfg *config.Config, download repository.Object, torrentClient *torrentPkg.Client, repo *repository.BboltRepository) (Worker, error) {
	switch download.Type {
	case "http":
		var d http.Download

		err := json.Unmarshal(download.Data, &d)
		if err != nil {
			return nil, err
		}

		if d.Status == status.Active {
			d.Status = status.Paused
		}

		return http.New(ctx, cfg.Http, d.URL, &d, repo, d.Priority)

	case "torrent":
		var d torrent.Download

		err := json.Unmarshal(download.Data, &d)
		if err != nil {
			return nil, err
		}

		if d.Status == status.Active {
			d.Status = status.Paused
		}

		return torrent.New(ctx, &d, d.Url, d.IsMagnet, d.Dir, torrentClient, repo, d.Priority)

	default:
		return nil, ErrUnsupportedScheme
	}
}
