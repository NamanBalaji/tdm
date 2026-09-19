package manager_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

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

func (s *mockStore) SaveBatch(ctx context.Context, dls []*download.Download) error {
	for _, dl := range dls {
		if err := s.Save(ctx, dl); err != nil {
			return err
		}
	}

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
	startFn    func(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error
	removeCh   chan uuid.UUID
	initCalls  atomic.Int32
}

// namedDownloader is a mockDownloader registered under another Type().
type namedDownloader struct {
	*mockDownloader
	name string
}

func (d *namedDownloader) Type() string { return d.name }

func newMockDownloader() *mockDownloader {
	return &mockDownloader{
		canHandle: true,
		removeCh:  make(chan uuid.UUID, 8),
	}
}

func (d *mockDownloader) Type() string { return "mock" }

func (d *mockDownloader) CanHandle(_ string) bool { return d.canHandle }

func (d *mockDownloader) Init(_ context.Context, url string, priority int) (*download.Download, error) {
	d.initCalls.Add(1)

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

func (d *mockDownloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
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

	onProgress(dl.TotalSize, dl.TotalSize, nil)

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
			assert.NotEqual(t, uuid.Nil(), id)
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

		t.Run("duplicate URL of a live download is rejected", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()

			blockCh := make(chan struct{})
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
				select {
				case <-blockCh:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			m, cancel := startManager(t, s, dlr, 2)
			defer func() {
				close(blockCh)
				shutdownManager(t, m, cancel)
			}()

			url := "http://example.com/dup.zip"
			_, err := m.AddDownload(context.Background(), url, 5)
			require.NoError(t, err)

			// Two live rows for one URL would share a single torrent handle
			// (dropping one stalls the other), so the re-add must fail.
			_, err = m.AddDownload(context.Background(), url, 5)
			assert.ErrorIs(t, err, manager.ErrDuplicateURL)
		})

		t.Run("concurrent adds of the same URL race to exactly one record", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()

			// Block Start so the first record can't reach Completed (which
			// would legitimately allow a re-add) while the others race.
			blockCh := make(chan struct{})
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
				select {
				case <-blockCh:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			m, cancel := startManager(t, s, dlr, 2)
			defer func() {
				close(blockCh)
				shutdownManager(t, m, cancel)
			}()

			// Init is instantaneous in the mock, so widen the race window by
			// launching many adds at once — the pendingURLs reservation must
			// let exactly one through regardless of interleaving.
			url := "http://example.com/race.zip"

			var (
				wg    sync.WaitGroup
				okMu  sync.Mutex
				okays int
			)

			start := make(chan struct{})

			for range 8 {

				wg.Go(func() {
					<-start

					if _, err := m.AddDownload(context.Background(), url, 5); err == nil {
						okMu.Lock()
						okays++
						okMu.Unlock()
					} else {
						assert.ErrorIs(t, err, manager.ErrDuplicateURL)
					}
				})
			}

			close(start)
			wg.Wait()

			assert.Equal(t, 1, okays, "exactly one concurrent add may succeed")
		})

		t.Run("re-adding a completed URL is allowed", func(t *testing.T) {
			s := newMockStore()
			dlr := newMockDownloader()
			m, cancel := startManager(t, s, dlr, 2)
			defer shutdownManager(t, m, cancel)

			url := "http://example.com/again.zip"
			_, err := m.AddDownload(context.Background(), url, 5)
			require.NoError(t, err)

			// Default mock completes immediately.
			require.Eventually(t, func() bool {
				infos := m.GetAllDownloads()
				return len(infos) == 1 && infos[0].Status == download.Completed
			}, 5*time.Second, 10*time.Millisecond)

			_, err = m.AddDownload(context.Background(), url, 5)
			assert.NoError(t, err, "a completed record must not block a fresh download")
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
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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
				dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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

		ctx := t.Context()

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

		dlr.startFn = func(ctx context.Context, _ *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
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
				onProgress(1000, 1000, nil)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		m, cancel := startManager(t, s, dlr, 2)
		defer shutdownManager(t, m, cancel)

		ctx := context.Background()
		for i := range 5 {
			_, err := m.AddDownload(ctx, fmt.Sprintf("http://example.com/file-%d", i), i%10+1)
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
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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
			dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
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

// A URL whose content only reveals its real type once probed (e.g. a
// tracker link serving a .torrent) is handed from one downloader to another.
func TestAddDownloadHandoff(t *testing.T) {
	const url = "http://example.com/download.php?id=1"

	setup := func(t *testing.T, firstErr error) (*mockStore, *mockDownloader, *namedDownloader, *manager.Manager, context.CancelFunc) {
		t.Helper()

		s := newMockStore()

		first := newMockDownloader()
		first.initErr = firstErr

		// The handoff target doesn't claim the URL itself — only the probe
		// of the first downloader knows it should get it.
		second := &namedDownloader{mockDownloader: newMockDownloader(), name: "other"}
		second.canHandle = false
		second.initDL = &download.Download{
			ID:        uuid.New(),
			URL:       url,
			Filename:  "linux.iso",
			Status:    download.Pending,
			Priority:  5,
			Type:      "other",
			TotalSize: 1000,
		}

		ctx, cancel := context.WithCancel(context.Background())
		m := manager.New(s, 2)
		m.Register(first)
		m.Register(second)
		require.NoError(t, m.Start(ctx))

		return s, first, second, m, cancel
	}

	t.Run("follows the handoff to the named downloader", func(t *testing.T) {
		s, first, second, m, cancel := setup(t, &download.HandoffError{Type: "other", Reason: "serves a torrent"})
		defer shutdownManager(t, m, cancel)

		id, err := m.AddDownload(context.Background(), url, 5)
		require.NoError(t, err)

		persisted := s.get(id)
		require.NotNil(t, persisted)
		assert.Equal(t, "other", persisted.Type)
		assert.Equal(t, int32(1), first.initCalls.Load())
		assert.Equal(t, int32(1), second.initCalls.Load())
	})

	t.Run("handoff to an unregistered downloader fails cleanly", func(t *testing.T) {
		s, _, second, m, cancel := setup(t, &download.HandoffError{Type: "missing", Reason: "x"})
		defer shutdownManager(t, m, cancel)

		_, err := m.AddDownload(context.Background(), url, 5)

		var handoff *download.HandoffError
		require.ErrorAs(t, err, &handoff)
		assert.Equal(t, "missing", handoff.Type)
		assert.Equal(t, 0, s.count())
		assert.Equal(t, int32(0), second.initCalls.Load())
	})

	t.Run("handoff to itself is not followed", func(t *testing.T) {
		s, first, _, m, cancel := setup(t, &download.HandoffError{Type: "mock", Reason: "x"})
		defer shutdownManager(t, m, cancel)

		_, err := m.AddDownload(context.Background(), url, 5)
		require.Error(t, err)
		assert.Equal(t, int32(1), first.initCalls.Load())
		assert.Equal(t, 0, s.count())
	})

	t.Run("a second handoff is not followed", func(t *testing.T) {
		s, first, second, m, cancel := setup(t, &download.HandoffError{Type: "other", Reason: "x"})
		defer shutdownManager(t, m, cancel)

		// The target hands straight back: following it would loop forever.
		second.initErr = &download.HandoffError{Type: "mock", Reason: "y"}

		_, err := m.AddDownload(context.Background(), url, 5)
		require.Error(t, err)
		assert.Equal(t, int32(1), first.initCalls.Load())
		assert.Equal(t, int32(1), second.initCalls.Load())
		assert.Equal(t, 0, s.count())
	})
}

// Regression test: when a download fails because its persisted type has no
// registered downloader, the freed scheduler slot must be re-offered —
// otherwise downloads left Queued by the same scheduling pass starve until
// the next user action.
func TestUnknownTypeFailureReschedulesQueued(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	// Ghost record: unregistered type, higher priority — wins the only slot.
	ghost := &download.Download{
		ID:        uuid.New(),
		URL:       "http://example.com/ghost",
		Status:    download.Paused,
		Priority:  10,
		Type:      "ghost",
		TotalSize: 1000,
		CreatedAt: time.Now(),
	}
	healthy := &download.Download{
		ID:        uuid.New(),
		URL:       "http://example.com/healthy",
		Status:    download.Paused,
		Priority:  1,
		Type:      "mock",
		TotalSize: 1000,
		CreatedAt: time.Now(),
	}
	require.NoError(t, s.Save(context.Background(), ghost))
	require.NoError(t, s.Save(context.Background(), healthy))

	m, cancel := startManager(t, s, dlr, 1)
	defer shutdownManager(t, m, cancel)

	m.ResumeDownload(context.Background(), ghost.ID)
	m.ResumeDownload(context.Background(), healthy.ID)

	// Drain the ghost's error so the channel doesn't fill.
	go func() {
		for range m.GetErrors() { //nolint:revive // draining
		}
	}()

	require.Eventually(t, func() bool {
		for _, info := range m.GetAllDownloads() {
			if info.ID == healthy.ID {
				return info.Status == download.Completed
			}
		}

		return false
	}, 5*time.Second, 10*time.Millisecond, "healthy download must run after the ghost frees its slot")
}

// Regression test: a quick pause→resume while the previous Start goroutine
// is still unwinding must NOT start a second goroutine for the same
// download — two live Starts would share torrent handles and the old
// goroutine's completion would clobber the new one's cancel func.
func TestPauseResumeDoesNotDoubleStart(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	var (
		mu          sync.Mutex
		live        int
		maxLive     int
		slowUnwind  = 50 * time.Millisecond
		startedOnce = make(chan struct{}, 16)
	)

	dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
		mu.Lock()
		live++
		if live > maxLive {
			maxLive = live
		}
		mu.Unlock()

		startedOnce <- struct{}{}

		<-ctx.Done()
		// Simulate slow teardown (torrent Drop, state save) after cancel.
		time.Sleep(slowUnwind)

		mu.Lock()
		live--
		mu.Unlock()

		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := manager.New(s, 3)
	m.Register(dlr)
	require.NoError(t, m.Start(ctx))

	id, err := m.AddDownload(ctx, "http://example.com/file.zip", 5)
	require.NoError(t, err)

	select {
	case <-startedOnce:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}

	// Pause then resume immediately — well inside the unwind window.
	m.PauseDownload(ctx, id)
	m.ResumeDownload(ctx, id)

	// Wait for the second start (must happen only after the first exits).
	select {
	case <-startedOnce:
	case <-time.After(5 * time.Second):
		t.Fatal("download was never restarted after resume")
	}

	mu.Lock()
	observed := maxLive
	mu.Unlock()
	assert.Equal(t, 1, observed, "at most one Start goroutine per download")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, m.Shutdown(shutdownCtx))
}

// Regression test: removing a record whose output path is shared with
// another record (same URL re-downloaded after completion) must not call
// the downloader's Remove — that would delete the other record's files or
// drop its live torrent handle.
func TestRemoveSkipsSharedOutput(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	// Two records sharing Dir+Filename: an old Completed row and a live one.
	oldDL := &download.Download{
		ID:        uuid.New(),
		URL:       "http://example.com/shared.bin",
		Filename:  "shared.bin",
		Dir:       "/tmp/dl",
		Status:    download.Completed,
		Priority:  5,
		Type:      "mock",
		TotalSize: 100,
		CreatedAt: time.Now(),
	}
	liveDL := &download.Download{
		ID:        uuid.New(),
		URL:       "http://example.com/shared.bin",
		Filename:  "shared.bin",
		Dir:       "/tmp/dl",
		Status:    download.Paused,
		Priority:  5,
		Type:      "mock",
		TotalSize: 100,
		CreatedAt: time.Now(),
	}
	require.NoError(t, s.Save(context.Background(), oldDL))
	require.NoError(t, s.Save(context.Background(), liveDL))

	m, cancel := startManager(t, s, dlr, 2)
	defer shutdownManager(t, m, cancel)

	m.RemoveDownload(context.Background(), oldDL.ID)

	select {
	case id := <-dlr.removeCh:
		t.Fatalf("downloader Remove must not run for a shared output (got %s)", id)
	case <-time.After(200 * time.Millisecond):
	}

	// The record itself is still gone from list and store.
	assert.Len(t, m.GetAllDownloads(), 1)
	assert.Nil(t, s.get(oldDL.ID))

	// Removing the last reference must clean up normally.
	m.RemoveDownload(context.Background(), liveDL.ID)

	select {
	case id := <-dlr.removeCh:
		assert.Equal(t, liveDL.ID, id)
	case <-time.After(2 * time.Second):
		t.Fatal("downloader Remove should run for the last reference")
	}
}

// Regression test: a persist-tick snapshot taken just before a download
// completes must never commit AFTER (and clobber) the completion record —
// both paths clear the dirty flag, so a clobbered record would stay stale
// in the store until shutdown and resurrect a finished download as Active
// after a crash.
func TestCompletionNotClobberedByPersistTick(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	dlr.startFn = func(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
		onProgress(500, 1000, jsontext.Value(`{"mid":"flight"}`))
		time.Sleep(2 * time.Millisecond)
		onProgress(1000, 1000, jsontext.Value(`{"all":"done"}`))

		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := manager.New(s, 8)
	m.Register(dlr)
	require.NoError(t, m.Start(ctx))

	// Hammer the persist path the way the 1s ticker would, concurrently
	// with completions.
	stopSaves := make(chan struct{})
	savesDone := make(chan struct{})

	go func() {
		defer close(savesDone)

		for {
			select {
			case <-stopSaves:
				return
			default:
				m.SaveAllForTest(ctx)
			}
		}
	}()

	ids := make([]uuid.UUID, 0, 30)

	for i := range 30 {
		id, err := m.AddDownload(ctx, fmt.Sprintf("http://example.com/file-%d.zip", i), 5)
		require.NoError(t, err)

		ids = append(ids, id)
	}

	require.Eventually(t, func() bool {
		for _, id := range ids {
			persisted := s.get(id)
			if persisted == nil || persisted.Status != download.Completed {
				return false
			}
		}

		return true
	}, 10*time.Second, 10*time.Millisecond, "every completion must reach the store despite concurrent persist ticks")

	close(stopSaves)
	<-savesDone

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, m.Shutdown(shutdownCtx))
}

// Regression test: removing an active download must not let the unwinding
// download goroutine (or an in-flight persist batch) write the record back
// after store.Delete — a "ghost" download on next launch.
func TestRemovedDownloadIsNotResurrected(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	started := make(chan struct{}, 1)
	dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
		started <- struct{}{}
		<-ctx.Done()
		// Simulate slow unwinding (saveState etc.) after cancellation so
		// handleCompletion runs well after RemoveDownload's store.Delete.
		time.Sleep(20 * time.Millisecond)

		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := manager.New(s, 2)
	m.Register(dlr)
	require.NoError(t, m.Start(ctx))

	id, err := m.AddDownload(ctx, "http://example.com/file.zip", 5)
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}

	m.RemoveDownload(ctx, id)

	// Give the unwinding goroutine time to (incorrectly) resurrect it.
	time.Sleep(200 * time.Millisecond)
	assert.Nil(t, s.get(id), "removed download must stay deleted")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, m.Shutdown(shutdownCtx))
}

// Regression test: in-flight progress and resume-state checkpoints must reach
// the store while Start is still running — otherwise a SIGKILL or power loss
// throws away everything downloaded since the download began.
func TestCheckpointPersistedWhileDownloadRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()

		stateBlob := jsontext.Value(`{"chunks":"in-flight"}`)
		dlr.startFn = func(ctx context.Context, _ *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
			onProgress(400, 1000, stateBlob)
			<-ctx.Done()

			return ctx.Err()
		}

		ctx, cancel := context.WithCancel(context.Background())
		m := manager.New(s, 2)
		m.Register(dlr)
		require.NoError(t, m.Start(ctx))

		id, err := m.AddDownload(ctx, "http://example.com/file.zip", 5)
		require.NoError(t, err)

		// Cross a persist tick while the download is still running.
		time.Sleep(2100 * time.Millisecond)

		persisted := s.get(id)
		require.NotNil(t, persisted)
		assert.Equal(t, download.Active, persisted.Status)
		assert.Equal(t, int64(400), persisted.Downloaded)
		assert.Equal(t, stateBlob, persisted.State)

		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		require.NoError(t, m.Shutdown(shutdownCtx))
	})
}

// Regression test: downloader State writes must not race the persist ticker.
func TestDownloaderStateWriteDoesNotRacePersistLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newMockStore()
		dlr := newMockDownloader()

		stateBlob := []byte(`{"resume":"data"}`)
		dlr.startFn = func(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
			for {
				select {
				case <-ctx.Done():
					dl.State = stateBlob

					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
					dl.State = stateBlob
					onProgress(500, 1000, nil)
				}
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		m := manager.New(s, 2)
		m.Register(dlr)
		require.NoError(t, m.Start(ctx))

		id, err := m.AddDownload(ctx, "http://example.com/file.zip", 5)
		require.NoError(t, err)

		time.Sleep(2100 * time.Millisecond)

		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		require.NoError(t, m.Shutdown(shutdownCtx))

		persisted := s.get(id)
		require.NotNil(t, persisted)
		assert.Equal(t, stateBlob, []byte(persisted.State))
	})
}

// Regression test: an error reported after Shutdown times out must not panic.
func TestShutdownTimeoutLateErrorDoesNotPanic(t *testing.T) {
	s := newMockStore()
	dlr := newMockDownloader()

	blockCh := make(chan struct{})
	lateErr := errors.New("disk write failed during cleanup")
	dlr.startFn = func(ctx context.Context, _ *download.Download, _ func(int64, int64, jsontext.Value)) error {
		<-ctx.Done()
		<-blockCh

		return lateErr
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := manager.New(s, 2)
	m.Register(dlr)
	require.NoError(t, m.Start(ctx))

	_, err := m.AddDownload(ctx, "http://example.com/file.zip", 5)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shutdownCancel()
	require.ErrorIs(t, m.Shutdown(shutdownCtx), manager.ErrShutdownTimeout)

	close(blockCh)

	select {
	case dlErr := <-m.GetErrors():
		assert.ErrorIs(t, dlErr.Error, lateErr)
	case <-time.After(time.Second):
		t.Fatal("expected the late error to be reported")
	}

	infos := m.GetAllDownloads()
	require.Len(t, infos, 1)
	assert.Equal(t, download.Paused, infos[0].Status)
}
