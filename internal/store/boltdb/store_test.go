package boltdb_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/store/boltdb"
)

func setup(t *testing.T) (*boltdb.Store, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "boltdb_store_test")
	require.NoError(t, err)

	dbPath := filepath.Join(dir, "test.db")
	store, err := boltdb.New(dbPath)
	require.NoError(t, err)

	cleanup := func() {
		assert.NoError(t, store.Close())
		assert.NoError(t, os.RemoveAll(dir))
	}

	return store, cleanup
}

func makeDownload(url, filename string) *download.Download {
	return &download.Download{
		ID:       uuid.New(),
		URL:      url,
		Filename: filename,
		Dir:      "/tmp",
		Status:   download.Pending,
		Type:     "http",
	}
}

func TestNew(t *testing.T) {
	t.Run("creates store and db file", func(t *testing.T) {
		store, cleanup := setup(t)
		defer cleanup()

		assert.NotNil(t, store)
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		_, err := boltdb.New("/nonexistent/path/test.db")
		assert.Error(t, err)
	})
}

func TestStore(t *testing.T) {
	t.Run("Save", func(t *testing.T) {
		testCases := []struct {
			name     string
			download *download.Download
			wantErr  bool
		}{
			{
				name:     "valid http download",
				download: makeDownload("http://example.com/file.zip", "file.zip"),
			},
			{
				name: "valid torrent download",
				download: &download.Download{
					ID:       uuid.New(),
					URL:      "magnet:?xt=urn:btih:abc",
					Filename: "movie.mkv",
					Dir:      "/tmp",
					Status:   download.Queued,
					Type:     "torrent",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				store, cleanup := setup(t)
				defer cleanup()

				ctx := context.Background()
				err := store.Save(ctx, tc.download)
				if tc.wantErr {
					assert.Error(t, err)
					return
				}

				require.NoError(t, err)

				all, err := store.GetAll(ctx)
				require.NoError(t, err)
				require.Len(t, all, 1)
				assert.Equal(t, tc.download.ID, all[0].ID)
				assert.Equal(t, tc.download.URL, all[0].URL)
				assert.Equal(t, tc.download.Filename, all[0].Filename)
			})
		}
	})

	t.Run("Save overwrites existing", func(t *testing.T) {
		store, cleanup := setup(t)
		defer cleanup()

		ctx := context.Background()
		dl := makeDownload("http://example.com/file.zip", "file.zip")

		require.NoError(t, store.Save(ctx, dl))

		dl.Status = download.Active
		dl.Downloaded = 500
		require.NoError(t, store.Save(ctx, dl))

		all, err := store.GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, download.Active, all[0].Status)
		assert.Equal(t, int64(500), all[0].Downloaded)
	})

	t.Run("GetAll", func(t *testing.T) {
		testCases := []struct {
			name      string
			seedCount int
		}{
			{"empty store returns empty slice", 0},
			{"single download", 1},
			{"multiple downloads", 3},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				store, cleanup := setup(t)
				defer cleanup()

				ctx := context.Background()
				ids := make(map[uuid.UUID]bool)

				for i := range tc.seedCount {
					dl := makeDownload("http://example.com/file", "file"+string(rune('A'+i)))
					require.NoError(t, store.Save(ctx, dl))
					ids[dl.ID] = true
				}

				all, err := store.GetAll(ctx)
				require.NoError(t, err)
				assert.Len(t, all, tc.seedCount)

				for _, dl := range all {
					assert.True(t, ids[dl.ID], "unexpected download ID %s", dl.ID)
				}
			})
		}
	})

	t.Run("Delete", func(t *testing.T) {
		testCases := []struct {
			name          string
			deleteID      func(seeded uuid.UUID) uuid.UUID
			wantErr       bool
			wantRemaining int
		}{
			{
				name:          "existing download",
				deleteID:      func(id uuid.UUID) uuid.UUID { return id },
				wantErr:       false,
				wantRemaining: 1,
			},
			{
				name:          "non-existent download is a no-op",
				deleteID:      func(_ uuid.UUID) uuid.UUID { return uuid.New() },
				wantErr:       false,
				wantRemaining: 2,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				store, cleanup := setup(t)
				defer cleanup()

				ctx := context.Background()
				d1 := makeDownload("http://example.com/a", "a.zip")
				d2 := makeDownload("http://example.com/b", "b.zip")
				require.NoError(t, store.Save(ctx, d1))
				require.NoError(t, store.Save(ctx, d2))

				err := store.Delete(ctx, tc.deleteID(d1.ID))
				if tc.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)

				all, err := store.GetAll(ctx)
				require.NoError(t, err)
				assert.Len(t, all, tc.wantRemaining)
			})
		}
	})

	t.Run("Close", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "boltdb_close_test")
		require.NoError(t, err)
		defer os.RemoveAll(dir)

		store, err := boltdb.New(filepath.Join(dir, "test.db"))
		require.NoError(t, err)

		assert.NoError(t, store.Close())

		err = store.Save(context.Background(), makeDownload("http://example.com/f", "f"))
		assert.Error(t, err, "operations after Close should fail")
	})
}
