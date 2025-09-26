package http_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	httpPkg "github.com/NamanBalaji/tdm/internal/http"
	"github.com/NamanBalaji/tdm/internal/repository"
	statusPkg "github.com/NamanBalaji/tdm/internal/status"
)

func createTestRepository(t *testing.T) *repository.BboltRepository {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	t.Cleanup(func() { assert.NoError(t, os.Remove(tmpFile.Name())) })
	assert.NoError(t, tmpFile.Close())

	repo, err := repository.NewBboltRepository(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to create repository: %v", err)
	}
	t.Cleanup(func() { assert.NoError(t, repo.Close()) })

	return repo
}

func createTestServer(t *testing.T, data []byte) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Accept-Ranges", "bytes")

		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_, err := w.Write(data)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server
}

func createTestServerWithDelay(t *testing.T, data []byte, delay time.Duration) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(data)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return server
}

func createTestServerWithError(t *testing.T, statusCode int) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNew(t *testing.T) {
	data := []byte("test data for download")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	tests := []struct {
		name     string
		url      string
		priority int
		wantErr  bool
	}{
		{
			name:     "create new worker with valid URL",
			url:      server.URL + "/test.txt",
			priority: 5,
			wantErr:  false,
		},
		{
			name:     "create worker with different priority",
			url:      server.URL + "/test2.txt",
			priority: 8,
			wantErr:  false,
		},
		{
			name:     "create worker with minimum priority",
			url:      server.URL + "/test3.txt",
			priority: 1,
			wantErr:  false,
		},
		{
			name:     "create worker with maximum priority",
			url:      server.URL + "/test4.txt",
			priority: 10,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			worker, err := httpPkg.New(ctx, tt.url, nil, repo, tt.priority)

			if tt.wantErr {
				if err == nil {
					t.Errorf("New() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if worker == nil {
				t.Errorf("New() returned nil worker")
				return
			}

			if worker.GetPriority() != tt.priority {
				t.Errorf("getPriority() = %v, want %v", worker.GetPriority(), tt.priority)
			}

			if worker.GetID().String() == "" {
				t.Errorf("GetID() returned empty UUID")
			}

			if worker.GetFilename() == "" {
				t.Errorf("GetFilename() returned empty string")
			}

			initialStatus := worker.GetStatus()
			if initialStatus != statusPkg.Pending {
				t.Errorf("Initial status = %v, want %v", initialStatus, statusPkg.Pending)
			}
		})
	}
}

func TestNew_WithInvalidURL(t *testing.T) {
	repo := createTestRepository(t)
	ctx := context.Background()

	errorServer := createTestServerWithError(t, http.StatusNotFound)
	_, err := httpPkg.New(ctx, errorServer.URL+"/notfound.txt", nil, repo, 5)
	if err == nil {
		t.Errorf("New() with 404 URL should return error but got none")
	}
}

func TestWorker_GetMethods(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 7)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	t.Run("getPriority", func(t *testing.T) {
		priority := worker.GetPriority()
		if priority != 7 {
			t.Errorf("getPriority() = %v, want %v", priority, 7)
		}
	})

	t.Run("GetID", func(t *testing.T) {
		id := worker.GetID()
		if id.String() == "" {
			t.Errorf("GetID() returned empty UUID")
		}
	})

	t.Run("getStatus", func(t *testing.T) {
		status := worker.GetStatus()
		if status != statusPkg.Pending {
			t.Errorf("getStatus() = %v, want %v", status, statusPkg.Pending)
		}
	})

	t.Run("GetFilename", func(t *testing.T) {
		filename := worker.GetFilename()
		if filename == "" {
			t.Errorf("GetFilename() returned empty string")
		}
	})

	t.Run("Done", func(t *testing.T) {
		done := worker.Done()
		if done == nil {
			t.Errorf("Done() returned nil channel")
		}
	})
}

func TestWorker_Queue(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	worker.Queue()

	status := worker.GetStatus()
	if status != statusPkg.Queued {
		t.Errorf("After Queue(), getStatus() = %v, want %v", status, statusPkg.Queued)
	}
}

func TestWorker_Pause(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  statusPkg.Status
		setupFunc      func(*httpPkg.Worker)
		expectedStatus statusPkg.Status
		expectError    bool
	}{
		{
			name:           "pause pending download (no change)",
			initialStatus:  statusPkg.Pending,
			setupFunc:      func(w *httpPkg.Worker) {},
			expectedStatus: statusPkg.Pending,
			expectError:    false,
		},
		{
			name:           "pause queued download",
			initialStatus:  statusPkg.Queued,
			setupFunc:      func(w *httpPkg.Worker) { w.Queue() },
			expectedStatus: statusPkg.Paused,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("test")
			server := createTestServer(t, data)
			repo := createTestRepository(t)

			ctx := context.Background()
			worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
			if err != nil {
				t.Fatalf("Failed to create worker: %v", err)
			}

			tt.setupFunc(worker)

			err = worker.Pause()
			if tt.expectError && err == nil {
				t.Errorf("Pause() expected error but got none")
				return
			}

			if !tt.expectError && err != nil {
				t.Errorf("Pause() error = %v", err)
				return
			}

			status := worker.GetStatus()
			if status != tt.expectedStatus {
				t.Errorf("After Pause(), getStatus() = %v, want %v", status, tt.expectedStatus)
			}
		})
	}
}

func TestWorker_PauseActiveDownload(t *testing.T) {
	data := []byte("test data for active pause")
	server := createTestServerWithDelay(t, data, 50*time.Millisecond)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if worker.GetStatus() != statusPkg.Active {
		t.Errorf("Expected status to be Active after Start(), got %v", worker.GetStatus())
	}

	err = worker.Pause()
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}

	<-worker.Done()

	finalStatus := worker.GetStatus()
	if finalStatus != statusPkg.Paused {
		t.Errorf("After pausing active download, status = %v, want %v", finalStatus, statusPkg.Paused)
	}
}

func TestWorker_Cancel(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Cancel()
	if err != nil {
		t.Errorf("Cancel() error = %v", err)
	}

	status := worker.GetStatus()
	if status != statusPkg.Cancelled {
		t.Errorf("After Cancel(), getStatus() = %v, want %v", status, statusPkg.Cancelled)
	}
}

func TestWorker_Start(t *testing.T) {
	data := []byte("small test data for completion")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status := worker.GetStatus()
	if status != statusPkg.Active {
		t.Errorf("After Start(), status = %v, want %v", status, statusPkg.Active)
	}

	err = <-worker.Done()
	if err != nil {
		t.Errorf("Download failed: %v", err)
	}

	finalStatus := worker.GetStatus()
	if finalStatus != statusPkg.Completed {
		t.Errorf("Final status = %v, want %v", finalStatus, statusPkg.Completed)
	}

	progress := worker.Progress()
	if progress.GetPercentage() != 100.0 {
		t.Errorf("Progress percentage = %v, want %v", progress.GetPercentage(), 100.0)
	}

	if progress.GetTotalSize() != int64(len(data)) {
		t.Errorf("Progress total size = %v, want %v", progress.GetTotalSize(), len(data))
	}

	if progress.GetDownloaded() != int64(len(data)) {
		t.Errorf("Progress downloaded = %v, want %v", progress.GetDownloaded(), len(data))
	}
}

func TestWorker_StartAlreadyStarted(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() error = %v", err)
	}

	err = worker.Start(ctx)
	if err == nil {
		t.Errorf("Second Start() should return error but got none")
	}

	assert.NoError(t, worker.Cancel())
	<-worker.Done()
}

func TestWorker_StartWithCompletedStatus(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	assert.NoError(t, worker.Start(ctx))
	<-worker.Done()

	err = worker.Start(ctx)
	if err != nil {
		t.Errorf("Start() on completed download should not error, got: %v", err)
	}
}

func TestWorker_PauseAndResume(t *testing.T) {
	data := make([]byte, 1024)
	server := createTestServerWithDelay(t, data, 50*time.Millisecond)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if worker.GetStatus() != statusPkg.Active {
		t.Fatalf("Expected status to be Active after Start(), got %v", worker.GetStatus())
	}

	err = worker.Pause()
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}

	<-worker.Done()

	if worker.GetStatus() != statusPkg.Paused {
		t.Fatalf("Expected status to be Paused after pause, got %v", worker.GetStatus())
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Errorf("Resume() error = %v", err)
	}

	if worker.GetStatus() != statusPkg.Active {
		t.Errorf("After Resume(), status = %v, want %v", worker.GetStatus(), statusPkg.Active)
	}

	<-worker.Done()

	if worker.GetStatus() != statusPkg.Completed {
		t.Errorf("Final status = %v, want %v", worker.GetStatus(), statusPkg.Completed)
	}
}

func TestWorker_Remove(t *testing.T) {
	data := []byte("test")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Remove()

	status := worker.GetStatus()
	if status != statusPkg.Cancelled {
		t.Errorf("After Remove(), status = %v, want %v", status, statusPkg.Cancelled)
	}
}

func TestWorker_Progress(t *testing.T) {
	data := []byte("test progress data content")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	progress := worker.Progress()

	if progress.GetDownloaded() != 0 {
		t.Errorf("Initial downloaded = %v, want %v", progress.GetDownloaded(), 0)
	}

	if progress.GetPercentage() != 0.0 {
		t.Errorf("Initial percentage = %v, want %v", progress.GetPercentage(), 0.0)
	}

	assert.NoError(t, worker.Start(ctx))
	<-worker.Done()

	finalProgress := worker.Progress()
	if finalProgress.GetPercentage() != 100.0 {
		t.Errorf("Final percentage = %v, want %v", finalProgress.GetPercentage(), 100.0)
	}

	if finalProgress.GetTotalSize() != int64(len(data)) {
		t.Errorf("Final total size = %v, want %v", finalProgress.GetTotalSize(), len(data))
	}

	if finalProgress.GetDownloaded() != int64(len(data)) {
		t.Errorf("Final downloaded = %v, want %v", finalProgress.GetDownloaded(), len(data))
	}
}

func TestWorker_CancelDuringDownload(t *testing.T) {
	data := make([]byte, 1024*1024)
	server := createTestServerWithDelay(t, data, 100*time.Millisecond)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/large.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = worker.Cancel()
	if err != nil {
		t.Errorf("Cancel() error = %v", err)
	}

	<-worker.Done()

	status := worker.GetStatus()
	if status != statusPkg.Cancelled {
		t.Errorf("Final status = %v, want %v", status, statusPkg.Cancelled)
	}
}

func TestWorker_PauseDuringDownload(t *testing.T) {
	data := make([]byte, 1024*1024)
	server := createTestServerWithDelay(t, data, 100*time.Millisecond)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/large.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	err = worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = worker.Pause()
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		<-worker.Done()
		close(done)
	}()

	select {
	case <-done:
	}

	status := worker.GetStatus()
	if status != statusPkg.Paused {
		t.Errorf("Status after pause = %v, want %v", status, statusPkg.Paused)
	}
}

func TestWorker_ConcurrentOperations(t *testing.T) {
	data := []byte("test concurrent operations")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(5)

	go func() {
		defer wg.Done()
		worker.GetStatus()
	}()

	go func() {
		defer wg.Done()
		worker.GetPriority()
	}()

	go func() {
		defer wg.Done()
		worker.GetFilename()
	}()

	go func() {
		defer wg.Done()
		worker.Progress()
	}()

	go func() {
		defer wg.Done()
		worker.Queue()
	}()

	wg.Wait()
}

func TestWorker_ErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		serverFunc func(*testing.T) *httptest.Server
		expectErr  bool
	}{
		{
			name: "404 not found",
			serverFunc: func(t *testing.T) *httptest.Server {
				return createTestServerWithError(t, http.StatusNotFound)
			},
			expectErr: true,
		},
		{
			name: "500 internal server error",
			serverFunc: func(t *testing.T) *httptest.Server {
				return createTestServerWithError(t, http.StatusInternalServerError)
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.serverFunc(t)
			repo := createTestRepository(t)

			ctx := context.Background()
			_, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)

			if tt.expectErr && err == nil {
				t.Errorf("Expected error but got none")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestWorker_StatusTransitions(t *testing.T) {
	data := []byte("test status transitions")
	server := createTestServer(t, data)
	repo := createTestRepository(t)

	ctx := context.Background()
	worker, err := httpPkg.New(ctx, server.URL+"/test.txt", nil, repo, 5)
	if err != nil {
		t.Fatalf("Failed to create worker: %v", err)
	}

	if worker.GetStatus() != statusPkg.Pending {
		t.Errorf("Initial status = %v, want %v", worker.GetStatus(), statusPkg.Pending)
	}

	worker.Queue()
	if worker.GetStatus() != statusPkg.Queued {
		t.Errorf("After Queue(), status = %v, want %v", worker.GetStatus(), statusPkg.Queued)
	}

	assert.NoError(t, worker.Pause())
	if worker.GetStatus() != statusPkg.Paused {
		t.Errorf("After Pause(), status = %v, want %v", worker.GetStatus(), statusPkg.Paused)
	}

	assert.NoError(t, worker.Start(ctx))
	if worker.GetStatus() != statusPkg.Active {
		t.Errorf("After Resume(), status = %v, want %v", worker.GetStatus(), statusPkg.Active)
	}

	<-worker.Done()
	if worker.GetStatus() != statusPkg.Completed {
		t.Errorf("Final status = %v, want %v", worker.GetStatus(), statusPkg.Completed)
	}
}
