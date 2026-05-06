package manager_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/manager"
)

// mockStore implements store.Store in-memory for testing.
type mockStore struct {
	mu        sync.Mutex
	downloads map[uuid.UUID]*download.Download
	saveErr   error
	getAllErr error
	deleteErr error
}

func newMockStore() *mockStore {
	return &mockStore{downloads: make(map[uuid.UUID]*download.Download)}
}

func (s *mockStore) Save(_ context.Context, dl *download.Download) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.saveErr != nil {
		return s.saveErr
	}

	cp := *dl
	s.downloads[dl.ID] = &cp

	return nil
}

func (s *mockStore) GetAll(_ context.Context) ([]*download.Download, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.getAllErr != nil {
		return nil, s.getAllErr
	}

	result := make([]*download.Download, 0, len(s.downloads))
	for _, dl := range s.downloads {
		cp := *dl
		result = append(result, &cp)
	}

	return result, nil
}

func (s *mockStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deleteErr != nil {
		return s.deleteErr
	}

	delete(s.downloads, id)

	return nil
}

func (s *mockStore) Close() error { return nil }

func (s *mockStore) get(id uuid.UUID) *download.Download {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.downloads[id]
}

func (s *mockStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.downloads)
}

// mockDownloader implements download.Downloader for testing.
type mockDownloader struct {
	canHandle  bool
	initDL     *download.Download
	initErr    error
	startErr   error
	startDelay time.Duration
	startFn    func(ctx context.Context, dl *download.Download, onProgress func(int64, int64)) error
	removeCh   chan uuid.UUID
}

func newMockDownloader() *mockDownloader {
	return &mockDownloader{
		canHandle: true,
		removeCh:  make(chan uuid.UUID, 8),
	}
}

func (d *mockDownloader) Type() string { return "mock" }

func (d *mockDownloader) CanHandle(_ string) bool { return d.canHandle }

func (d *mockDownloader) Init(_ context.Context, url string, priority int) (*download.Download, error) {
	if d.initErr != nil {
		return nil, d.initErr
	}

	if d.initDL != nil {
		return d.initDL, nil
	}

	return &download.Download{
		ID:        uuid.New(),
		URL:       url,
		Filename:  "test-file",
		Dir:       "/tmp",
		Status:    download.Pending,
		Priority:  priority,
		Type:      "mock",
		TotalSize: 1000,
	}, nil
}

func (d *mockDownloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64)) error {
	if d.startFn != nil {
		return d.startFn(ctx, dl, onProgress)
	}

	if d.startDelay > 0 {
		select {
		case <-time.After(d.startDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if d.startErr != nil {
		return d.startErr
	}

	onProgress(dl.TotalSize, dl.TotalSize)

	return nil
}

func (d *mockDownloader) Remove(dl *download.Download) error {
	d.removeCh <- dl.ID

	return nil
}

func startManager(t *testing.T, s *mockStore, dlr *mockDownloader, maxConc int) (*manager.Manager, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	m := manager.New(s, maxConc)
	m.Register(dlr)

	err := m.Start(ctx)
	require.NoError(t, err)

	return m, cancel
}

func shutdownManager(t *testing.T, m *manager.Manager, cancel context.CancelFunc) {
	t.Helper()

	cancel()

	ctx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	err := m.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestManager(t *testing.T) {
	t.Run("AddDownload", func(t *testing.T) {
		t.Run("valid download", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			id, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			require.NoError(t, err)
			assert.NotEqual(t, uuid.Nil, id)
			assert.Equal(t, 1, s.count())
		})

		t.Run("invalid priority", func(t *testing.T) {
			testCases := []struct {
				name     string
				priority int
			}{
				{"too low", 0},
				{"too high", 11},
				{"negative", -1},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					s := newMockStore()
					dlr := newMockDownloader()
					m, cancel := startManager(t, s, dlr, 2)
					defer shutdownManager(t, m, cancel)

					_, err := m.AddDownload(context.Background(), "http://example.com/file.zip", tc.priority)
					assert.ErrorIs(t, err, manager.ErrInvalidPriority)
				})
			}
		})

		t.Run("no downloader can handle URL", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dlr.canHandle = false
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			_, err := m.AddDownload(context.Background(), "ftp://example.com/file", 5)
			assert.ErrorIs(t, err, manager.ErrNoDownloader)
		})

		t.Run("init error", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dlr.initErr = errors.New("probe failed")
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			_, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			assert.Error(t, err)
			assert.Equal(t, 0, s.count())
		})

		t.Run("store save error", func(t *testing.T) {
			s := newMockStore()
			s.saveErr = errors.New("disk full")
			dlr := newMockDownloader()
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			_, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			assert.Error(t, err)
		})
	})

	t.Run("PauseDownload", func(t *testing.T) {
		testCases := []struct {
			name          string
			initialStatus download.Status
			expectPaused  bool
		}{
			{"pauses active download", download.Active, true},
			{"pauses queued download", download.Queued, true},
			{"no-op on completed download", download.Completed, false},
			{"no-op on paused download", download.Paused, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				s := newMockStore()
				dlr := newMockDownloader()

				blockCh := make(chan struct{})
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
					select {
					case <-blockCh:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				dl := &download.Download{
					ID:        uuid.New(),
					Status:    tc.initialStatus,
					Priority:  5,
					Type:      "mock",
					TotalSize: 1000,
					CreatedAt: time.Now(),
				}
				s.Save(context.Background(), dl)

				m, cancel := startManager(t, s, dlr, 2)
				defer func() {
					close(blockCh)
					shutdownManager(t, m, cancel)
				}()

				// Give scheduler time to pick up active/queued downloads
				time.Sleep(50 * time.Millisecond)

				m.PauseDownload(context.Background(), dl.ID)
				time.Sleep(50 * time.Millisecond)

				infos := m.GetAllDownloads()
				require.Len(t, infos, 1)

				if tc.expectPaused {
					assert.Equal(t, download.Paused, infos[0].Status)
				}
			})
		}
	})

	t.Run("ResumeDownload", func(t *testing.T) {
		testCases := []struct {
			name          string
			initialStatus download.Status
			expectQueued  bool
		}{
			{"resumes paused download", download.Paused, true},
			{"resumes failed download", download.Failed, true},
			{"no-op on completed download", download.Completed, false},
			{"no-op on active download", download.Active, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				s := newMockStore()
				dlr := newMockDownloader()

				blockCh := make(chan struct{})
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
					select {
					case <-blockCh:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				dl := &download.Download{
					ID:        uuid.New(),
					Status:    tc.initialStatus,
					Priority:  5,
					Type:      "mock",
					TotalSize: 1000,
					CreatedAt: time.Now(),
				}
				s.Save(context.Background(), dl)

				m, cancel := startManager(t, s, dlr, 2)
				defer func() {
					close(blockCh)
					shutdownManager(t, m, cancel)
				}()

				time.Sleep(50 * time.Millisecond)

				m.ResumeDownload(context.Background(), dl.ID)
				time.Sleep(50 * time.Millisecond)

				infos := m.GetAllDownloads()
				require.Len(t, infos, 1)

				if tc.expectQueued {
					s := infos[0].Status
					assert.True(t, s == download.Queued || s == download.Active,
						"expected Queued or Active, got %s", s)
				}
			})
		}
	})

	t.Run("CancelDownload", func(t *testing.T) {
		testCases := []struct {
			name            string
			initialStatus   download.Status
			expectCancelled bool
		}{
			{"cancels active download", download.Active, true},
			{"cancels queued download", download.Queued, true},
			{"cancels paused download", download.Paused, true},
			{"no-op on completed download", download.Completed, false},
			{"no-op on already cancelled download", download.Cancelled, false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				s := newMockStore()
				dlr := newMockDownloader()

				blockCh := make(chan struct{})
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
					select {
					case <-blockCh:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				dl := &download.Download{
					ID:        uuid.New(),
					Status:    tc.initialStatus,
					Priority:  5,
					Type:      "mock",
					TotalSize: 1000,
					CreatedAt: time.Now(),
				}
				s.Save(context.Background(), dl)

				m, cancel := startManager(t, s, dlr, 2)
				defer func() {
					close(blockCh)
					shutdownManager(t, m, cancel)
				}()

				time.Sleep(50 * time.Millisecond)

				m.CancelDownload(context.Background(), dl.ID)
				time.Sleep(50 * time.Millisecond)

				infos := m.GetAllDownloads()
				require.Len(t, infos, 1)

				if tc.expectCancelled {
					assert.Equal(t, download.Cancelled, infos[0].Status)
				}
			})
		}
	})

	t.Run("RemoveDownload", func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()
		m, cancel := startManager(t, s, dlr, 2)
		defer shutdownManager(t, m, cancel)

		id, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		m.RemoveDownload(context.Background(), id)
		time.Sleep(50 * time.Millisecond)

		infos := m.GetAllDownloads()
		assert.Empty(t, infos)
		assert.Nil(t, s.get(id))

		select {
		case removedID := <-dlr.removeCh:
			assert.Equal(t, id, removedID)
		case <-time.After(time.Second):
			t.Fatal("expected Remove to be called on the downloader")
		}
	})

	t.Run("RemoveDownload non-existent is no-op", func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()
		m, cancel := startManager(t, s, dlr, 2)
		defer shutdownManager(t, m, cancel)

		m.RemoveDownload(context.Background(), uuid.New())
		assert.Empty(t, m.GetAllDownloads())
	})

	t.Run("GetAllDownloads", func(t *testing.T) {
		t.Run("returns sorted by priority desc", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
				<-ctx.Done()
				return ctx.Err()
			}
			m, cancel := startManager(t, s, dlr, 10)
			defer shutdownManager(t, m, cancel)

			ctx := context.Background()
			_, err := m.AddDownload(ctx, "http://example.com/low", 1)
			require.NoError(t, err)
			_, err = m.AddDownload(ctx, "http://example.com/high", 9)
			require.NoError(t, err)
			_, err = m.AddDownload(ctx, "http://example.com/mid", 5)
			require.NoError(t, err)

			time.Sleep(50 * time.Millisecond)

			infos := m.GetAllDownloads()
			require.Len(t, infos, 3)
			assert.Equal(t, 9, infos[0].Priority)
			assert.Equal(t, 5, infos[1].Priority)
			assert.Equal(t, 1, infos[2].Priority)
		})

		t.Run("completed download shows 100%", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dl := &download.Download{
				ID:         uuid.New(),
				Filename:   "done.zip",
				Status:     download.Completed,
				Priority:   5,
				Type:       "mock",
				TotalSize:  1000,
				Downloaded: 1000,
				CreatedAt:  time.Now(),
			}
			s.Save(context.Background(), dl)

			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			infos := m.GetAllDownloads()
			require.Len(t, infos, 1)
			assert.Equal(t, 100.0, infos[0].Progress.Percentage)
		})
	})

	t.Run("Start resets active downloads to paused", func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()

		dl := &download.Download{
			ID:        uuid.New(),
			Status:    download.Active,
			Priority:  5,
			Type:      "mock",
			TotalSize: 1000,
			CreatedAt: time.Now(),
		}
		s.Save(context.Background(), dl)

		m, cancel := startManager(t, s, dlr, 2)
		defer shutdownManager(t, m, cancel)

		infos := m.GetAllDownloads()
		require.Len(t, infos, 1)
		assert.Equal(t, download.Paused, infos[0].Status)
	})

	t.Run("Start returns error when store fails", func(t *testing.T) {
		s := newMockStore()
		s.getAllErr = errors.New("db corrupt")
		dlr := newMockDownloader()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		m := manager.New(s, 2)
		m.Register(dlr)

		err := m.Start(ctx)
		assert.Error(t, err)
	})

	t.Run("download completion", func(t *testing.T) {
		t.Run("successful download sets completed status", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			id, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			require.NoError(t, err)

			time.Sleep(200 * time.Millisecond)

			infos := m.GetAllDownloads()
			require.Len(t, infos, 1)
			assert.Equal(t, download.Completed, infos[0].Status)

			persisted := s.get(id)
			require.NotNil(t, persisted)
			assert.Equal(t, download.Completed, persisted.Status)
		})

		t.Run("failed download sets failed status and sends error", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dlr.startErr = errors.New("network error")
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			id, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			require.NoError(t, err)

			time.Sleep(200 * time.Millisecond)

			infos := m.GetAllDownloads()
			require.Len(t, infos, 1)
			assert.Equal(t, download.Failed, infos[0].Status)

			select {
			case dlErr := <-m.GetErrors():
				assert.Equal(t, id, dlErr.ID)
				assert.Error(t, dlErr.Error)
			case <-time.After(time.Second):
				t.Fatal("expected error on error channel")
			}
		})
	})

	t.Run("concurrency limit", func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()

		activeMu := sync.Mutex{}
		activeCount := 0
		maxSeen := 0
		doneCh := make(chan struct{})

		dlr.startFn = func(ctx context.Context, _ *download.Download, onProgress func(int64, int64)) error {
			activeMu.Lock()
			activeCount++
			if activeCount > maxSeen {
				maxSeen = activeCount
			}
			activeMu.Unlock()

			defer func() {
				activeMu.Lock()
				activeCount--
				activeMu.Unlock()
			}()

			select {
			case <-doneCh:
				onProgress(1000, 1000)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		m, cancel := startManager(t, s, dlr, 2)
		defer shutdownManager(t, m, cancel)

		ctx := context.Background()
		for i := range 5 {
			_, err := m.AddDownload(ctx, "http://example.com/file", i%10+1)
			require.NoError(t, err)
		}

		time.Sleep(200 * time.Millisecond)

		activeMu.Lock()
		observed := maxSeen
		activeMu.Unlock()

		assert.LessOrEqual(t, observed, 2)

		close(doneCh)
	})

	t.Run("Shutdown", func(t *testing.T) {
		t.Run("graceful shutdown saves all downloads", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
				<-ctx.Done()
				return ctx.Err()
			}

			m, cancel := startManager(t, s, dlr, 2)

			_, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			require.NoError(t, err)

			time.Sleep(100 * time.Millisecond)

			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()

			err = m.Shutdown(shutdownCtx)
			assert.NoError(t, err)
			assert.Equal(t, 1, s.count())
		})

		t.Run("shutdown timeout returns error", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()

			blockCh := make(chan struct{})
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64)) error {
				select {
				case <-blockCh:
					return nil
				case <-ctx.Done():
					// Simulate slow cleanup — don't return immediately on cancel
					<-blockCh
					return ctx.Err()
				}
			}

			ctx, cancel := context.WithCancel(context.Background())
			m := manager.New(s, 2)
			m.Register(dlr)
			require.NoError(t, m.Start(ctx))

			_, err := m.AddDownload(context.Background(), "http://example.com/file.zip", 5)
			require.NoError(t, err)

			time.Sleep(100 * time.Millisecond)

			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer shutdownCancel()

			err = m.Shutdown(shutdownCtx)
			assert.ErrorIs(t, err, manager.ErrShutdownTimeout)

			close(blockCh)
		})
	})
}
