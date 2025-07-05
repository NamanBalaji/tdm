package repository_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/repository"
)

type mockDownload struct {
	id      uuid.UUID
	T       string `json:"type"`
	content string
	fail    bool
}

func (m *mockDownload) GetID() uuid.UUID {
	return m.id
}

func (m *mockDownload) Type() string {
	return m.T
}

func (m *mockDownload) MarshalJSON() ([]byte, error) {
	if m.fail {
		return nil, errors.New("marshal error")
	}
	return json.Marshal(&struct {
		Content string `json:"content"`
	}{
		Content: m.content,
	})
}

func setup(t *testing.T) (*repository.BboltRepository, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "bbolt_test")
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

func TestNewBboltRepository(t *testing.T) {
	t.Run("creates a new repository and db file", func(t *testing.T) {
		repo, cleanup := setup(t)
		defer cleanup()
		assert.NotNil(t, repo)
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		_, err := repository.NewBboltRepository("/nonexistent/path/test.db")
		assert.Error(t, err)
	})
}

func TestBboltRepository_Save_GetAll(t *testing.T) {
	repo, cleanup := setup(t)
	defer cleanup()

	tests := []struct {
		name     string
		download repository.Download
		wantErr  bool
	}{
		{
			name: "save a valid download",
			download: &mockDownload{
				id:      uuid.New(),
				T:       "mock",
				content: "some data",
			},
			wantErr: false,
		},
		{
			name: "save another valid download",
			download: &mockDownload{
				id:      uuid.New(),
				T:       "mock2",
				content: "more data",
			},
			wantErr: false,
		},
		{
			name: "save download that fails to marshal",
			download: &mockDownload{
				id:   uuid.New(),
				fail: true,
			},
			wantErr: true,
		},
	}

	savedDownloads := make(map[string]repository.Object)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Save(tt.download)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			raw, _ := tt.download.MarshalJSON()
			savedDownloads[tt.download.GetID().String()] = repository.Object{
				Type: tt.download.Type(),
				Data: raw,
			}

			all, err := repo.GetAll()
			require.NoError(t, err)
			assert.Len(t, all, len(savedDownloads))
			assert.True(t, reflect.DeepEqual(savedDownloads, all))
		})
	}
}

func TestBboltRepository_Delete(t *testing.T) {
	repo, cleanup := setup(t)
	defer cleanup()

	d1 := &mockDownload{id: uuid.New(), content: "download 1"}
	d2 := &mockDownload{id: uuid.New(), content: "download 2"}

	require.NoError(t, repo.Save(d1))
	require.NoError(t, repo.Save(d2))

	t.Run("delete existing download", func(t *testing.T) {
		err := repo.Delete(d1.GetID().String())
		assert.NoError(t, err)

		all, err := repo.GetAll()
		require.NoError(t, err)
		assert.Len(t, all, 1)
		_, exists := all[d1.GetID().String()]
		assert.False(t, exists)
	})

	t.Run("delete non-existent download", func(t *testing.T) {
		err := repo.Delete(uuid.New().String())
		assert.Error(t, err)
		assert.Equal(t, repository.ErrDownloadNotFound, err)
	})
}
