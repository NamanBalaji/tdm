package worker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpWorker "github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/worker"
)

func setup(t *testing.T) (*repository.BboltRepository, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "worker_test_repo")
	require.NoError(t, err)

	dbPath := filepath.Join(dir, "test.db")
	repo, err := repository.NewBboltRepository(dbPath)
	require.NoError(t, err)

	cleanup := func() {
		assert.NoError(t, repo.Close())
		assert.NoError(t, os.RemoveAll(dir))
	}

	return repo, cleanup
}

func TestGetWorker(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/downloadable", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1024")
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

	repo, cleanup := setup(t)
	defer cleanup()

	testCases := []struct {
		name          string
		url           string
		priority      int
		expectWorker  bool
		expectedError error
	}{
		{
			name:          "Valid HTTP URL",
			url:           server.URL + "/downloadable",
			priority:      5,
			expectWorker:  true,
			expectedError: nil,
		},
		{
			name:          "Non-downloadable content type",
			url:           server.URL + "/not-downloadable",
			priority:      5,
			expectWorker:  false,
			expectedError: worker.ErrUnsupportedScheme,
		},
		{
			name:          "Server error on HEAD request",
			url:           server.URL + "/server-error",
			priority:      5,
			expectWorker:  false,
			expectedError: errors.New("server error (5xx)"),
		},
		{
			name:          "Unsupported scheme FTP",
			url:           "ftp://example.com/file.zip",
			priority:      3,
			expectWorker:  false,
			expectedError: worker.ErrUnsupportedScheme,
		},
		{
			name:          "Invalid URL format",
			url:           "://invalid-url",
			priority:      10,
			expectWorker:  false,
			expectedError: errors.New("parse \"://invalid-url\": missing protocol scheme"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w, err := worker.GetWorker(ctx, tc.url, tc.priority, repo)

			if tc.expectWorker {
				assert.NoError(t, err)
				require.NotNil(t, w)
				_, ok := w.(*httpWorker.Worker)
				assert.True(t, ok, "worker should be of type *http.Worker")
				assert.Equal(t, tc.priority, w.GetPriority())
			} else {
				assert.Nil(t, w)
				require.Error(t, err)
				if tc.expectedError != nil {
					if tc.name == "Invalid URL format" {
						assert.Contains(t, err.Error(), "missing protocol scheme")
					} else if tc.name == "Server error on HEAD request" {
						assert.Contains(t, err.Error(), "server error (5xx)")
					} else {
						assert.EqualError(t, err, tc.expectedError.Error())
					}
				}
			}
		})
	}
}
