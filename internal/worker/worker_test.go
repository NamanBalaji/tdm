package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpWorker "github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/torrent"
	"github.com/NamanBalaji/tdm/internal/worker"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

func setup(t *testing.T) (*repository.BboltRepository, *torrentPkg.Client, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	repo, err := repository.NewBboltRepository(dbPath)
	require.NoError(t, err)

	cfg := config.DefaultConfig().Torrent
	cfg.MetainfoTimeout = 1
	cfg.DownloadDir = t.TempDir()

	torrentClient, err := torrentPkg.NewClient(cfg)
	require.NoError(t, err)

	cleanup := func() {
		assert.NoError(t, repo.Close())
		assert.NoError(t, torrentClient.Close())
	}

	return repo, torrentClient, cleanup
}

func TestGetWorker(t *testing.T) {
	// Create test HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/downloadable", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/torrent", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Header().Set("Content-Length", "512")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/not-downloadable", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/server-error", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	repo, torrentClient, cleanup := setup(t)
	defer cleanup()

	tests := []struct {
		name         string
		url          string
		priority     int
		expectWorker bool
		workerType   string
		wantErr      bool
	}{
		{
			name:         "Valid HTTP downloadable URL",
			url:          server.URL + "/downloadable",
			priority:     5,
			expectWorker: true,
			workerType:   "http",
			wantErr:      false,
		},
		{
			name:         "HTTP torrent file URL",
			url:          server.URL + "/torrent",
			priority:     7,
			expectWorker: false, // Will fail due to invalid torrent content
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Valid magnet link",
			url:          "magnet:?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12",
			priority:     2,
			expectWorker: false, // Will fail due to timeout waiting for metadata
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Invalid magnet link",
			url:          "magnet:invalid",
			priority:     1,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Non-downloadable content type",
			url:          server.URL + "/not-downloadable",
			priority:     5,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Server error on HEAD request",
			url:          server.URL + "/server-error",
			priority:     5,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Unsupported scheme FTP",
			url:          "ftp://example.com/file.zip",
			priority:     3,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Invalid URL format",
			url:          "://invalid-url",
			priority:     10,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Empty URL",
			url:          "",
			priority:     1,
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := config.DefaultConfig()

			w, err := worker.GetWorker(ctx, &cfg, tt.url, tt.priority, torrentClient, repo)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, w)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, w)
				assert.Equal(t, tt.priority, w.GetPriority())

				// Check worker type
				switch tt.workerType {
				case "http":
					_, ok := w.(*httpWorker.Worker)
					assert.True(t, ok, "worker should be of type *http.Worker")
				case "torrent":
					_, ok := w.(*torrent.Worker)
					assert.True(t, ok, "worker should be of type *torrent.Worker")
				}
			}
		})
	}
}

func TestGetWorker_EdgeCases(t *testing.T) {
	repo, _, cleanup := setup(t)
	defer cleanup()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "URL with special characters",
			url:     "http://example.com/file with spaces.zip",
			wantErr: true, // Will fail on network request
		},
		{
			name:    "Very long URL",
			url:     "http://example.com/" + string(make([]byte, 2000)),
			wantErr: true, // Will fail on network request
		},
		{
			name:    "URL with query parameters",
			url:     "http://example.com/file.zip?param=value&other=123",
			wantErr: true, // Will fail on network request
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := config.DefaultConfig()

			w, err := worker.GetWorker(ctx, &cfg, tt.url, 1, nil, repo)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, w)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, w)
			}
		})
	}
}

func TestLoadWorker(t *testing.T) {
	repo, torrentClient, cleanup := setup(t)
	defer cleanup()

	tests := []struct {
		name         string
		downloadType string
		downloadData interface{}
		expectWorker bool
		workerType   string
		wantErr      bool
	}{
		{
			name:         "Load HTTP worker",
			downloadType: "http",
			downloadData: httpWorker.Download{
				Id:       uuid.New(),
				URL:      "http://example.com/file.zip",
				Filename: "file.zip",
				Status:   status.Paused,
				Priority: 5,
			},
			expectWorker: true,
			workerType:   "http",
			wantErr:      false,
		},
		{
			name:         "Load torrent worker",
			downloadType: "torrent",
			downloadData: torrent.Download{
				Id:       uuid.New(),
				Name:     "movie.mkv",
				Url:      "http://example.com/movie.torrent",
				IsMagnet: false,
				Status:   status.Paused,
				Priority: 3,
				Protocol: "torrent",
			},
			expectWorker: true,
			workerType:   "torrent",
			wantErr:      false,
		},
		{
			name:         "Load torrent worker from magnet",
			downloadType: "torrent",
			downloadData: torrent.Download{
				Id:       uuid.New(),
				Name:     "linux.iso",
				Url:      "magnet:?xt=urn:btih:abcdef1234567890",
				IsMagnet: true,
				Status:   status.Paused,
				Priority: 1,
				Protocol: "torrent",
			},
			expectWorker: true,
			workerType:   "torrent",
			wantErr:      false,
		},
		{
			name:         "Unsupported download type",
			downloadType: "ftp",
			downloadData: map[string]interface{}{
				"url": "ftp://example.com/file.zip",
			},
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
		{
			name:         "Invalid JSON data",
			downloadType: "http",
			downloadData: "invalid json string",
			expectWorker: false,
			workerType:   "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the download data
			data, err := json.Marshal(tt.downloadData)
			require.NoError(t, err)

			// Create repository object
			repoObj := repository.Object{
				Type: tt.downloadType,
				Data: data,
			}

			ctx := context.Background()
			cfg := config.DefaultConfig()

			w, err := worker.LoadWorker(ctx, &cfg, repoObj, torrentClient, repo)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, w)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, w)

				// Verify worker type
				switch tt.workerType {
				case "http":
					_, ok := w.(*httpWorker.Worker)
					assert.True(t, ok, "worker should be of type *http.Worker")
				case "torrent":
					_, ok := w.(*torrent.Worker)
					assert.True(t, ok, "worker should be of type *torrent.Worker")
				}

				// Verify status was reset from Active to Paused
				assert.NotEqual(t, status.Active, w.GetStatus())
			}
		})
	}
}

func TestLoadWorker_InvalidJSON(t *testing.T) {
	repo, _, cleanup := setup(t)
	defer cleanup()

	tests := []struct {
		name         string
		downloadType string
		jsonData     string
	}{
		{
			name:         "Malformed JSON",
			downloadType: "http",
			jsonData:     `{invalid json`,
		},
		{
			name:         "Malformed torrent JSON",
			downloadType: "torrent",
			jsonData:     `{invalid json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoObj := repository.Object{
				Type: tt.downloadType,
				Data: []byte(tt.jsonData),
			}

			ctx := context.Background()
			cfg := config.DefaultConfig()

			w, err := worker.LoadWorker(ctx, &cfg, repoObj, nil, repo)
			assert.Error(t, err)
			assert.Nil(t, w)
		})
	}
}

func TestWorker_ErrorConstants(t *testing.T) {
	assert.Equal(t, "unsupported scheme", worker.ErrUnsupportedScheme.Error())
}

func TestGetWorker_NilInputs(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		cfg     *config.Config
		url     string
		repo    *repository.BboltRepository
		wantErr bool
	}{
		{
			name:    "nil context",
			ctx:     nil,
			cfg:     nil,
			url:     "http://example.com/file.zip",
			repo:    nil,
			wantErr: true, // Will panic or error on nil context
		},
		{
			name:    "nil repo",
			ctx:     context.Background(),
			cfg:     nil,
			url:     "http://example.com/file.zip",
			repo:    nil,
			wantErr: true, // Will fail on network request anyway
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					// Expected for nil context
					assert.Contains(t, tt.name, "nil context")
				}
			}()

			w, err := worker.GetWorker(tt.ctx, tt.cfg, tt.url, 1, nil, tt.repo)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, w)
			}
		})
	}
}
