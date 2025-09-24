package engine_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/repository"
)

type EngineTestSuite struct {
	suite.Suite
	repo   *repository.BboltRepository
	engine *engine.Engine
	server *httptest.Server
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *EngineTestSuite) SetupSuite() {
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", "1024")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			content := make([]byte, 1024)
			for i := range content {
				content[i] = byte(i % 256)
			}
			_, err := w.Write(content)
			assert.NoError(s.T(), err)

		case "/large-file.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", "10240")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			content := make([]byte, 10240)
			_, err := w.Write(content)
			assert.NoError(s.T(), err)

		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", "512")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			content := make([]byte, 512)
			_, err := w.Write(content)
			assert.NoError(s.T(), err)
		}
	}))
}

func (s *EngineTestSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *EngineTestSuite) SetupTest() {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	require.NoError(s.T(), err)
	require.NoError(s.T(), tmpFile.Close())

	s.repo, err = repository.NewBboltRepository(tmpFile.Name())
	require.NoError(s.T(), err)

	s.engine = engine.NewEngine(s.repo, nil, 3)
	require.NotNil(s.T(), s.engine)

	s.ctx, s.cancel = context.WithTimeout(context.Background(), 30*time.Second)

	err = s.engine.Start(s.ctx)
	require.NoError(s.T(), err)

	s.T().Cleanup(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.repo != nil {
			assert.NoError(s.T(), s.repo.Close())
		}
		assert.NoError(s.T(), os.Remove(tmpFile.Name()))
	})
}

func (s *EngineTestSuite) TearDownTest() {
	if s.engine != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(s.T(), s.engine.Shutdown(shutdownCtx))
	}
}

func (s *EngineTestSuite) waitForCondition(condition func() bool, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if condition() {
				return true
			}
		}
	}
}

func (s *EngineTestSuite) waitForError(timeout time.Duration) *engine.DownloadError {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case err := <-s.engine.GetErrors():
		return &err
	case <-ctx.Done():
		return nil
	}
}

func (s *EngineTestSuite) waitForDownloadCount(expected int, timeout time.Duration) bool {
	return s.waitForCondition(func() bool {
		return len(s.engine.GetAllDownloads()) == expected
	}, timeout)
}

func (s *EngineTestSuite) waitForDownloadToExist(id uuid.UUID, timeout time.Duration) bool {
	return s.waitForCondition(func() bool {
		downloads := s.engine.GetAllDownloads()
		for _, d := range downloads {
			if d.ID == id {
				return true
			}
		}
		return false
	}, timeout)
}

func (s *EngineTestSuite) assertDownloadExists(id uuid.UUID, msgAndArgs ...interface{}) {
	downloads := s.engine.GetAllDownloads()
	found := false
	for _, d := range downloads {
		if d.ID == id {
			found = true
			break
		}
	}
	assert.True(s.T(), found, msgAndArgs...)
}

func (s *EngineTestSuite) requireValidDownloadID(id uuid.UUID, msgAndArgs ...interface{}) {
	require.NotEqual(s.T(), uuid.Nil, id, msgAndArgs...)
	require.True(s.T(), s.waitForDownloadToExist(id, 2*time.Second), "download should appear within timeout")
}

func (s *EngineTestSuite) TestNewEngine() {
	testCases := []struct {
		name          string
		maxConcurrent int
	}{
		{"zero concurrent", 0},
		{"one concurrent", 1},
		{"multiple concurrent", 5},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			tmpFile, err := os.CreateTemp("", "test_*.db")
			require.NoError(s.T(), err)
			defer assert.NoError(s.T(), os.Remove(tmpFile.Name()))
			require.NoError(s.T(), tmpFile.Close())

			repo, err := repository.NewBboltRepository(tmpFile.Name())
			require.NoError(s.T(), err)
			defer assert.NoError(s.T(), repo.Close())

			eng := engine.NewEngine(repo, nil, tc.maxConcurrent)
			assert.NotNil(s.T(), eng)
			assert.NotNil(s.T(), eng.GetErrors(), "error channel should be available")
		})
	}
}

func (s *EngineTestSuite) TestEngine_Start() {
	downloads := s.engine.GetAllDownloads()
	assert.Empty(s.T(), downloads, "should start with 0 downloads in empty repo")
}

func (s *EngineTestSuite) TestEngine_AddDownload() {
	testCases := []struct {
		name        string
		url         string
		priority    int
		expectError bool
		errorType   error
	}{
		{
			name:        "valid download",
			url:         s.server.URL + "/file.zip",
			priority:    5,
			expectError: false,
		},
		{
			name:        "priority too low",
			url:         s.server.URL + "/file.zip",
			priority:    0,
			expectError: true,
			errorType:   engine.ErrInvalidPriority,
		},
		{
			name:        "priority too high",
			url:         s.server.URL + "/file.zip",
			priority:    11,
			expectError: true,
			errorType:   engine.ErrInvalidPriority,
		},
		{
			name:        "invalid URL scheme",
			url:         "ftp://example.com/file.zip",
			priority:    5,
			expectError: true,
		},
		{
			name:        "non-existent file",
			url:         s.server.URL + "/notfound",
			priority:    5,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			id := s.engine.AddDownload(s.ctx, tc.url, tc.priority)

			if tc.expectError {
				assert.Equal(s.T(), uuid.Nil, id, "should return nil UUID for error case")

				downloadErr := s.waitForError(2 * time.Second)
				require.NotNil(s.T(), downloadErr, "should receive error on error channel")

				if tc.errorType != nil {
					assert.ErrorIs(s.T(), downloadErr.Error, tc.errorType)
				}
			} else {
				s.requireValidDownloadID(id, "should return valid UUID for successful add")

				downloads := s.engine.GetAllDownloads()
				require.NotEmpty(s.T(), downloads, "should have at least one download")

				var found *engine.DownloadInfo
				for _, d := range downloads {
					if d.ID == id {
						found = &d
						break
					}
				}
				require.NotNil(s.T(), found, "should find the added download")
				assert.Equal(s.T(), tc.priority, found.Priority, "priority should match")
			}
		})
	}
}

func (s *EngineTestSuite) TestEngine_PauseDownload() {
	s.Run("pause existing download", func() {
		id := s.engine.AddDownload(s.ctx, s.server.URL+"/file.zip", 5)
		s.requireValidDownloadID(id, "failed to add download")

		s.engine.PauseDownload(s.ctx, id)
		s.assertDownloadExists(id, "download should still exist after pause")
	})

	s.Run("pause non-existent download", func() {
		nonExistentID := uuid.New()
		assert.NotPanics(s.T(), func() {
			s.engine.PauseDownload(s.ctx, nonExistentID)
		}, "should not panic when pausing non-existent download")
	})
}

func (s *EngineTestSuite) TestEngine_ResumeDownload() {
	s.Run("resume paused download", func() {
		id := s.engine.AddDownload(s.ctx, s.server.URL+"/file.zip", 5)
		s.requireValidDownloadID(id, "failed to add download")

		s.engine.PauseDownload(s.ctx, id)
		s.engine.ResumeDownload(s.ctx, id)

		s.assertDownloadExists(id, "download should exist after resume")
	})

	s.Run("resume non-existent download", func() {
		nonExistentID := uuid.New()
		assert.NotPanics(s.T(), func() {
			s.engine.ResumeDownload(s.ctx, nonExistentID)
		}, "should not panic when resuming non-existent download")
	})
}

func (s *EngineTestSuite) TestEngine_CancelDownload() {
	s.Run("cancel existing download", func() {
		id := s.engine.AddDownload(s.ctx, s.server.URL+"/file.zip", 5)
		s.requireValidDownloadID(id, "failed to add download")

		s.engine.CancelDownload(s.ctx, id)
		s.assertDownloadExists(id, "download should still exist after cancel")
	})

	s.Run("cancel non-existent download", func() {
		nonExistentID := uuid.New()
		assert.NotPanics(s.T(), func() {
			s.engine.CancelDownload(s.ctx, nonExistentID)
		}, "should not panic when canceling non-existent download")
	})
}

func (s *EngineTestSuite) TestEngine_RemoveDownload() {
	s.Run("remove existing download", func() {
		id := s.engine.AddDownload(s.ctx, s.server.URL+"/file.zip", 5)
		s.requireValidDownloadID(id, "failed to add download")

		initialCount := len(s.engine.GetAllDownloads())
		s.engine.RemoveDownload(s.ctx, id)

		assert.True(s.T(), s.waitForDownloadCount(initialCount-1, 3*time.Second))

		downloads := s.engine.GetAllDownloads()
		for _, d := range downloads {
			assert.NotEqual(s.T(), id, d.ID, "removed download should not exist")
		}
	})

	s.Run("remove non-existent download", func() {
		nonExistentID := uuid.New()
		assert.NotPanics(s.T(), func() {
			s.engine.RemoveDownload(s.ctx, nonExistentID)
		}, "should not panic when removing non-existent download")
	})
}

func (s *EngineTestSuite) TestEngine_GetProgress() {
	s.Run("get progress for existing download", func() {
		id := s.engine.AddDownload(s.ctx, s.server.URL+"/file.zip", 5)
		s.requireValidDownloadID(id, "failed to add download")

		progress, err := s.engine.GetProgress(id)
		require.NoError(s.T(), err, "should not error when getting progress for existing download")
		require.NotNil(s.T(), progress, "progress should not be nil")

		assert.GreaterOrEqual(s.T(), progress.GetTotalSize(), int64(0), "total size should not be negative")
		assert.GreaterOrEqual(s.T(), progress.GetDownloaded(), int64(0), "downloaded should not be negative")
		assert.GreaterOrEqual(s.T(), progress.GetPercentage(), 0.0, "percentage should not be negative")
		assert.LessOrEqual(s.T(), progress.GetPercentage(), 100.0, "percentage should not exceed 100")
	})

	s.Run("get progress for non-existent download", func() {
		nonExistentID := uuid.New()
		_, err := s.engine.GetProgress(nonExistentID)
		assert.ErrorIs(s.T(), err, engine.ErrWorkerNotFound, "should return ErrWorkerNotFound")
	})
}

func (s *EngineTestSuite) TestEngine_GetAllDownloads() {
	priorities := []int{3, 8, 5, 1, 9}
	var addedIDs []uuid.UUID

	for i, priority := range priorities {
		url := fmt.Sprintf("%s/file%d.zip", s.server.URL, i)
		id := s.engine.AddDownload(s.ctx, url, priority)
		if id != uuid.Nil {
			addedIDs = append(addedIDs, id)
		}
	}

	assert.True(s.T(), s.waitForDownloadCount(len(addedIDs), 3*time.Second),
		"all downloads should appear within timeout")

	downloads := s.engine.GetAllDownloads()
	assert.Len(s.T(), downloads, len(addedIDs), "should have correct number of downloads")

	if len(downloads) > 1 {
		for i := 1; i < len(downloads); i++ {
			assert.GreaterOrEqual(s.T(), downloads[i-1].Priority, downloads[i].Priority,
				"downloads should be sorted by priority (descending)")
		}
	}

	downloadIDs := make(map[uuid.UUID]bool)
	for _, d := range downloads {
		downloadIDs[d.ID] = true
	}
	for _, id := range addedIDs {
		assert.True(s.T(), downloadIDs[id], "added download %s should be present", id)
	}
}

func (s *EngineTestSuite) TestEngine_ConcurrentOperations() {
	numWorkers := 3

	var wg sync.WaitGroup
	addResults := make(chan uuid.UUID, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			url := fmt.Sprintf("%s/file%d.zip", s.server.URL, idx)
			id := s.engine.AddDownload(s.ctx, url, (idx%5)+1)
			select {
			case addResults <- id:
			case <-s.ctx.Done():
				return
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(addResults)
	}()

	var addedIDs []uuid.UUID
	for id := range addResults {
		if id != uuid.Nil {
			addedIDs = append(addedIDs, id)
		}
	}

	assert.NotEmpty(s.T(), addedIDs, "should have some successful downloads")

	assert.True(s.T(), s.waitForDownloadCount(len(addedIDs), 3*time.Second),
		"downloads should appear within timeout")

	if len(addedIDs) > 0 {
		numOps := min(2, len(addedIDs))
		opDone := make(chan struct{}, numOps*3)

		for i := 0; i < numOps; i++ {
			id := addedIDs[i]

			go func(downloadID uuid.UUID) {
				s.engine.PauseDownload(s.ctx, downloadID)
				select {
				case opDone <- struct{}{}:
				case <-s.ctx.Done():
				}
			}(id)

			go func(downloadID uuid.UUID) {
				select {
				case <-time.After(50 * time.Millisecond):
					s.engine.ResumeDownload(s.ctx, downloadID)
				case <-s.ctx.Done():
					return
				}
				select {
				case opDone <- struct{}{}:
				case <-s.ctx.Done():
				}
			}(id)

			go func(downloadID uuid.UUID) {
				_, _ = s.engine.GetProgress(downloadID)
				select {
				case opDone <- struct{}{}:
				case <-s.ctx.Done():
				}
			}(id)
		}

		opsCompleted := 0
		expectedOps := numOps * 3

		operationTimeout := time.After(3 * time.Second)
		for opsCompleted < expectedOps {
			select {
			case <-opDone:
				opsCompleted++
			case <-operationTimeout:
				s.T().Logf("Operations completed: %d/%d", opsCompleted, expectedOps)
				finalDownloads := s.engine.GetAllDownloads()
				assert.NotEmpty(s.T(), finalDownloads, "should have downloads after concurrent operations")
				return
			case <-s.ctx.Done():
				s.T().Fatal("Test context cancelled")
			}
		}
	}
}

func (s *EngineTestSuite) TestEngine_ErrorHandling() {
	s.Run("invalid download URL", func() {
		id := s.engine.AddDownload(s.ctx, "invalid-url", 5)
		assert.Equal(s.T(), uuid.Nil, id, "should return nil UUID for invalid URL")

		downloadErr := s.waitForError(2 * time.Second)
		require.NotNil(s.T(), downloadErr, "should receive error on error channel")
		assert.NotNil(s.T(), downloadErr.Error, "error should have message")
	})
}

func (s *EngineTestSuite) TestEngine_Wait() {
	shutdownStarted := make(chan struct{})

	go func() {
		close(shutdownStarted)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(s.T(), s.engine.Shutdown(shutdownCtx))
	}()

	<-shutdownStarted

	waitDone := make(chan struct{})
	go func() {
		s.engine.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(6 * time.Second):
		s.T().Error("Wait() took too long to complete")
	}
}

func TestEngineTestSuite(t *testing.T) {
	suite.Run(t, new(EngineTestSuite))
}

func TestNewEngineEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		maxConcurrent int
		expectNil     bool
	}{
		{"negative concurrent", -1, false},
		{"large concurrent", 1000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test_*.db")
			require.NoError(t, err)
			defer assert.NoError(t, os.Remove(tmpFile.Name()))
			require.NoError(t, tmpFile.Close())

			repo, err := repository.NewBboltRepository(tmpFile.Name())
			require.NoError(t, err)
			defer assert.NoError(t, repo.Close())

			eng := engine.NewEngine(repo, nil, tt.maxConcurrent)
			if tt.expectNil {
				assert.Nil(t, eng)
			} else {
				assert.NotNil(t, eng)
			}
		})
	}
}
