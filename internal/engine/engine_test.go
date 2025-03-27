package engine_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/errors"
	"github.com/NamanBalaji/tdm/internal/protocol"
	"github.com/google/uuid"
)

type mockProtocol struct {
	canHandleFunc        func(url string) bool
	initializeFunc       func(url string, config *downloader.Config) (*common.DownloadInfo, error)
	createConnectionFunc func(url string, chunk *chunk.Chunk, config *downloader.Config) (connection.Connection, error)
}

func (m mockProtocol) CanHandle(url string) bool {
	if m.canHandleFunc != nil {
		return m.canHandleFunc(url)
	}
	return true
}

func (m mockProtocol) Initialize(url string, config *downloader.Config) (*common.DownloadInfo, error) {
	if m.initializeFunc != nil {
		return m.initializeFunc(url, config)
	}
	return &common.DownloadInfo{
		URL:            url,
		Filename:       "test.txt",
		TotalSize:      1000,
		SupportsRanges: true,
	}, nil
}

func (m mockProtocol) CreateConnection(url string, c *chunk.Chunk, config *downloader.Config) (connection.Connection, error) {
	if m.createConnectionFunc != nil {
		return m.createConnectionFunc(url, c, config)
	}
	return &mockConnection{}, nil
}

type mockConnection struct {
	connectFunc func() error
	readFunc    func(p []byte) (int, error)
	closeFunc   func() error
	isAliveFunc func() bool
	resetFunc   func() error
	urlFunc     func() string
	headersFunc func() map[string]string
	timeoutFunc func(time.Duration)
}

func (m *mockConnection) Connect() error {
	if m.connectFunc != nil {
		return m.connectFunc()
	}
	return nil
}

func (m *mockConnection) Read(p []byte) (int, error) {
	if m.readFunc != nil {
		return m.readFunc(p)
	}
	return len(p), io.EOF
}

func (m *mockConnection) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockConnection) IsAlive() bool {
	if m.isAliveFunc != nil {
		return m.isAliveFunc()
	}
	return true
}

func (m *mockConnection) Reset() error {
	if m.resetFunc != nil {
		return m.resetFunc()
	}
	return nil
}

func (m *mockConnection) GetURL() string {
	if m.urlFunc != nil {
		return m.urlFunc()
	}
	return "http://example.com/test"
}

func (m *mockConnection) GetHeaders() map[string]string {
	if m.headersFunc != nil {
		return m.headersFunc()
	}
	return map[string]string{}
}

func (m *mockConnection) SetTimeout(timeout time.Duration) {
	if m.timeoutFunc != nil {
		m.timeoutFunc(timeout)
	}
}

type mockProtocolHandler struct {
	getHandlerFunc func(url string) (protocol.Protocol, error)
	initializeFunc func(url string, config *downloader.Config) (*common.DownloadInfo, error)
}

func (m *mockProtocolHandler) GetHandler(url string) (protocol.Protocol, error) {
	if m.getHandlerFunc != nil {
		return m.getHandlerFunc(url)
	}
	return mockProtocol{}, nil
}

func (m *mockProtocolHandler) Initialize(url string, config *downloader.Config) (*common.DownloadInfo, error) {
	if m.initializeFunc != nil {
		return m.initializeFunc(url, config)
	}
	return &common.DownloadInfo{
		URL:            url,
		Filename:       "test.txt",
		TotalSize:      1000,
		SupportsRanges: true,
	}, nil
}

type mockRepository struct {
	saveFunc    func(download *downloader.Download) error
	findFunc    func(id uuid.UUID) (*downloader.Download, error)
	findAllFunc func() ([]*downloader.Download, error)
	deleteFunc  func(id uuid.UUID) error
	closeFunc   func() error
}

func (m *mockRepository) Save(download *downloader.Download) error {
	if m.saveFunc != nil {
		return m.saveFunc(download)
	}
	return nil
}

func (m *mockRepository) Find(id uuid.UUID) (*downloader.Download, error) {
	if m.findFunc != nil {
		return m.findFunc(id)
	}
	return nil, stderrors.New("download not found")
}

func (m *mockRepository) FindAll() ([]*downloader.Download, error) {
	if m.findAllFunc != nil {
		return m.findAllFunc()
	}
	return nil, nil
}

func (m *mockRepository) Delete(id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(id)
	}
	return nil
}

func (m *mockRepository) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

type mockChunkManager struct {
	createChunksFunc  func(downloadID uuid.UUID, filesize int64, supportsRange bool, maxConnections int, progressFn func(int64)) ([]*chunk.Chunk, error)
	mergeChunksFunc   func(chunks []*chunk.Chunk, targetPath string) error
	cleanupChunksFunc func(chunks []*chunk.Chunk) error
}

func createTestDownload() *downloader.Download {
	dl := &downloader.Download{
		ID:        uuid.New(),
		URL:       "http://example.com/test",
		Filename:  "test.txt",
		TotalSize: 1000,
		Status:    common.StatusPending,
		Config: &downloader.Config{
			Directory:   "/tmp",
			Connections: 2,
			MaxRetries:  3,
			RetryDelay:  time.Second,
		},
		Chunks: make([]*chunk.Chunk, 0),
	}

	// Create dummy chunks
	chunk1 := &chunk.Chunk{
		ID:         uuid.New(),
		DownloadID: dl.ID,
		StartByte:  0,
		EndByte:    499,
		Status:     common.StatusPending,
	}

	chunk2 := &chunk.Chunk{
		ID:         uuid.New(),
		DownloadID: dl.ID,
		StartByte:  500,
		EndByte:    999,
		Status:     common.StatusPending,
	}

	dl.Chunks = append(dl.Chunks, chunk1, chunk2)
	return dl
}

func simulateDownload(ctx context.Context, chunk *chunk.Chunk) error {
	if chunk.Connection == nil {
		return stderrors.New("connection is nil")
	}

	select {
	case <-ctx.Done():
		chunk.Status = common.StatusPaused
		return errors.NewContextError(ctx.Err(), "simulate")
	default:
		buf := make([]byte, 1024)
		_, err := chunk.Connection.Read(buf)
		if err != nil {
			chunk.Status = common.StatusFailed
			chunk.Error = err
			return err
		}

		chunk.Status = common.StatusCompleted
		return nil
	}
}

func TestEngineInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	config := engine.DefaultConfig()
	config.DownloadDir = tmpDir
	config.TempDir = filepath.Join(tmpDir, "temp")
	config.ConfigDir = filepath.Join(tmpDir, "config")

	eng, err := engine.New(config)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	if _, err := os.Stat(config.TempDir); os.IsNotExist(err) {
		t.Errorf("Temp dir was not created: %s", config.TempDir)
	}

	if config.DownloadDir != tmpDir {
		t.Errorf("Expected download dir %s, got %s", tmpDir, config.DownloadDir)
	}

	if err := eng.Shutdown(); err != nil {
		t.Fatalf("Failed to shutdown engine: %v", err)
	}
}

func TestConfig(t *testing.T) {
	config := engine.DefaultConfig()

	if config.MaxConcurrentDownloads <= 0 {
		t.Error("MaxConcurrentDownloads should be greater than 0")
	}

	if config.MaxConnectionsPerDownload <= 0 {
		t.Error("MaxConnectionsPerDownload should be greater than 0")
	}

	if config.ChunkSize <= 0 {
		t.Error("ChunkSize should be greater than 0")
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		expectedDownloadDir := filepath.Join(homeDir, "Downloads")
		if config.DownloadDir != expectedDownloadDir {
			t.Errorf("Expected default download dir %s, got %s", expectedDownloadDir, config.DownloadDir)
		}
	}

	tmpDir := t.TempDir()
	customConfig := engine.DefaultConfig()
	customConfig.DownloadDir = tmpDir

	if customConfig.DownloadDir != tmpDir {
		t.Errorf("Expected download dir %s, got %s", tmpDir, customConfig.DownloadDir)
	}
}

func TestDownloadChunkWithRetries_NetworkErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	download := createTestDownload()
	testChunk := download.Chunks[0]

	netErr := errors.NewNetworkError(stderrors.New("connection reset"), download.URL, true)

	mockConn := &mockConnection{
		readFunc: func(p []byte) (int, error) {
			return 0, netErr
		},
	}

	retryCount := 0

	err := func() error {
		testChunk.Status = common.StatusActive
		testChunk.Connection = mockConn

		err := simulateDownload(ctx, testChunk)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}

		if !errors.IsRetryable(err) {
			return err
		}

		for testChunk.RetryCount < download.Config.MaxRetries {
			testChunk.Reset()
			retryCount++

			testChunk.Connection = mockConn

			err = simulateDownload(ctx, testChunk)
			if err == nil || errors.Is(err, context.Canceled) {
				return err
			}

			if !errors.IsRetryable(err) {
				return err
			}
		}

		return fmt.Errorf("chunk failed after %d attempts: %w", download.Config.MaxRetries, err)
	}()

	if retryCount != download.Config.MaxRetries {
		t.Errorf("Expected %d retry attempts, got %d", download.Config.MaxRetries, retryCount)
	}

	if err == nil {
		t.Error("Expected error after retry attempts, got nil")
	}

	var downloadErr *errors.DownloadError
	if errors.As(err, &downloadErr) {
		if downloadErr.Category != errors.CategoryNetwork {
			t.Errorf("Expected error category %v, got %v", errors.CategoryNetwork, downloadErr.Category)
		}
	} else {
		t.Error("Expected error to be *errors.DownloadError")
	}
}

func TestDownloadChunkWithRetries_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	download := createTestDownload()
	testChunk := download.Chunks[0]

	mockConn := &mockConnection{
		readFunc: func(p []byte) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}

	testChunk.Connection = mockConn

	errChan := make(chan error, 1)
	go func() {
		errChan <- simulateDownload(ctx, testChunk)
	}()

	cancel()

	select {
	case err := <-errChan:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled error, got: %v", err)
		}

		var downloadErr *errors.DownloadError
		if errors.As(err, &downloadErr) {
			if downloadErr.Category != errors.CategoryContext {
				t.Errorf("Expected error category %v, got %v", errors.CategoryContext, downloadErr.Category)
			}
		} else {
			t.Error("Expected error to be *errors.DownloadError")
		}

	case <-time.After(time.Second):
		t.Error("Test timed out")
	}
}

func TestDownloadChunkWithRetries_NonRetryableErrors(t *testing.T) {
	ctx := context.Background()

	download := createTestDownload()
	testChunk := download.Chunks[0]

	resourceErr := errors.NewHTTPError(
		errors.ErrResourceNotFound,
		download.URL,
		404,
	)

	mockConn := &mockConnection{
		readFunc: func(p []byte) (int, error) {
			return 0, resourceErr
		},
	}

	testChunk.Connection = mockConn
	retryCount := 0

	err := func() error {
		err := simulateDownload(ctx, testChunk)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}

		if !errors.IsRetryable(err) {
			return err
		}

		for testChunk.RetryCount < download.Config.MaxRetries {
			testChunk.Reset()
			retryCount++

			testChunk.Connection = mockConn

			err = simulateDownload(ctx, testChunk)
			if err == nil || errors.Is(err, context.Canceled) {
				return err
			}

			if !errors.IsRetryable(err) {
				return err
			}
		}

		return fmt.Errorf("chunk failed after %d attempts: %w", download.Config.MaxRetries, err)
	}()

	if retryCount != 0 {
		t.Errorf("Expected 0 retry attempts for non-retryable error, got %d", retryCount)
	}

	if !errors.Is(err, resourceErr) {
		t.Errorf("Expected resource not found error, got: %v", err)
	}
}

func TestProcessDownload(t *testing.T) {
	download := createTestDownload()

	var statusMutex sync.Mutex
	var statusChanges []common.Status

	setStatus := func(status common.Status) {
		statusMutex.Lock()
		defer statusMutex.Unlock()
		statusChanges = append(statusChanges, status)
		download.Status = status
	}

	setStatus(download.Status)

	testCases := []struct {
		name           string
		setupFunc      func()
		expectedStatus common.Status
		expectError    bool
	}{
		{
			name: "Success case",
			setupFunc: func() {
				for _, c := range download.Chunks {
					c.Status = common.StatusCompleted
				}
				setStatus(common.StatusCompleted)
			},
			expectedStatus: common.StatusCompleted,
			expectError:    false,
		},
		{
			name: "Context cancellation",
			setupFunc: func() {
				setStatus(common.StatusPending)

				for _, c := range download.Chunks {
					c.Status = common.StatusPending
				}

				download.Error = errors.NewContextError(context.Canceled, download.URL)
			},
			expectedStatus: common.StatusPaused,
			expectError:    false,
		},
		{
			name: "Network failure",
			setupFunc: func() {
				setStatus(common.StatusPending)

				for _, c := range download.Chunks {
					c.Status = common.StatusPending
				}

				download.Error = errors.NewNetworkError(
					stderrors.New("connection reset"),
					download.URL,
					true,
				)
			},
			expectedStatus: common.StatusFailed,
			expectError:    true,
		},
		{
			name: "Resource error",
			setupFunc: func() {
				setStatus(common.StatusPending)

				for _, c := range download.Chunks {
					c.Status = common.StatusPending
				}

				download.Error = errors.NewHTTPError(
					errors.ErrResourceNotFound,
					download.URL,
					404,
				)
			},
			expectedStatus: common.StatusFailed,
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupFunc()

			if download.Error != nil {
				var downloadErr *errors.DownloadError
				if errors.As(download.Error, &downloadErr) && downloadErr.Category == errors.CategoryContext {
					setStatus(common.StatusPaused)
				} else {
					setStatus(common.StatusFailed)
				}
			}

			if download.Status != tc.expectedStatus {
				t.Errorf("Expected status %v, got %v", tc.expectedStatus, download.Status)
			}

			if tc.expectError && download.Error == nil {
				t.Error("Expected error but got nil")
			}

			if !tc.expectError && tc.expectedStatus != common.StatusCompleted && download.Error == nil {
				t.Error("Expected error for non-completed download but got nil")
			}
		})
	}
}

func TestErrorCategories(t *testing.T) {
	testCases := []struct {
		name        string
		err         error
		expectedCat errors.ErrorCategory
		isRetryable bool
	}{
		{
			name:        "Network error",
			err:         errors.NewNetworkError(stderrors.New("connection reset"), "http://example.com", true),
			expectedCat: errors.CategoryNetwork,
			isRetryable: true,
		},
		{
			name:        "Context canceled",
			err:         errors.NewContextError(context.Canceled, "http://example.com"),
			expectedCat: errors.CategoryContext,
			isRetryable: false,
		},
		{
			name:        "Resource error",
			err:         errors.NewHTTPError(errors.ErrResourceNotFound, "http://example.com", 404),
			expectedCat: errors.CategoryResource,
			isRetryable: false,
		},
		{
			name:        "Server error",
			err:         errors.NewHTTPError(stderrors.New("internal server error"), "http://example.com", 500),
			expectedCat: errors.CategoryProtocol,
			isRetryable: true,
		},
		{
			name:        "IO error",
			err:         errors.NewIOError(stderrors.New("permission denied"), "/tmp/file.txt"),
			expectedCat: errors.CategoryIO,
			isRetryable: false,
		},
		{
			name:        "Security error",
			err:         &errors.DownloadError{Err: stderrors.New("authentication failed"), Category: errors.CategorySecurity, Protocol: errors.ProtocolHTTP},
			expectedCat: errors.CategorySecurity,
			isRetryable: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var downloadErr *errors.DownloadError
			if !errors.As(tc.err, &downloadErr) {
				t.Fatalf("Expected *errors.DownloadError, got %T", tc.err)
			}

			if downloadErr.Category != tc.expectedCat {
				t.Errorf("Expected category %v, got %v", tc.expectedCat, downloadErr.Category)
			}

			if errors.IsRetryable(tc.err) != tc.isRetryable {
				t.Errorf("Expected isRetryable=%v, got %v", tc.isRetryable, errors.IsRetryable(tc.err))
			}
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	baseErr := errors.ErrResourceNotFound
	wrappedErr := errors.NewHTTPError(baseErr, "http://example.com", 404)

	if !errors.Is(wrappedErr, errors.ErrResourceNotFound) {
		t.Error("errors.Is should find the wrapped error")
	}

	var downloadErr *errors.DownloadError
	if !errors.As(wrappedErr, &downloadErr) {
		t.Error("errors.As should unwrap to DownloadError")
	}

	if downloadErr.Resource != "http://example.com" {
		t.Errorf("Expected resource http://example.com, got %s", downloadErr.Resource)
	}

	if downloadErr.StatusCode != 404 {
		t.Errorf("Expected status code 404, got %d", downloadErr.StatusCode)
	}
}

func TestNewDownloadError(t *testing.T) {
	// Test with nil error
	result := engine.NewDownloadError(nil, "test-resource")
	if result != nil {
		t.Errorf("Expected nil result for nil error, got %v", result)
	}

	cancelErr := context.Canceled
	result = engine.NewDownloadError(cancelErr, "test-resource")

	var downloadErr *errors.DownloadError
	if !errors.As(result, &downloadErr) {
		t.Fatalf("Expected *errors.DownloadError, got %T", result)
	}

	if downloadErr.Category != errors.CategoryContext {
		t.Errorf("Expected CategoryContext, got %v", downloadErr.Category)
	}

	genericErr := stderrors.New("generic error")
	result = engine.NewDownloadError(genericErr, "test-resource")

	if !errors.As(result, &downloadErr) {
		t.Fatalf("Expected *errors.DownloadError, got %T", result)
	}

	if downloadErr.Category != errors.CategoryUnknown {
		t.Errorf("Expected CategoryUnknown, got %v", downloadErr.Category)
	}

	if downloadErr.Resource != "test-resource" {
		t.Errorf("Expected resource 'test-resource', got %s", downloadErr.Resource)
	}
}

func TestIsRetryableError(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "Nil error",
			err:       nil,
			retryable: false,
		},
		{
			name:      "Network error (retryable)",
			err:       errors.NewNetworkError(stderrors.New("connection reset"), "test", true),
			retryable: true,
		},
		{
			name:      "Network error (non-retryable)",
			err:       errors.NewNetworkError(stderrors.New("host not found"), "test", false),
			retryable: false,
		},
		{
			name:      "Context error",
			err:       errors.NewContextError(context.Canceled, "test"),
			retryable: false,
		},
		{
			name:      "HTTP 500 error",
			err:       errors.NewHTTPError(stderrors.New("server error"), "test", 500),
			retryable: true,
		},
		{
			name:      "HTTP 404 error",
			err:       errors.NewHTTPError(stderrors.New("not found"), "test", 404),
			retryable: false,
		},
		{
			name:      "IO error",
			err:       errors.NewIOError(stderrors.New("disk full"), "test"),
			retryable: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := engine.IsRetryableError(tc.err)
			if result != tc.retryable {
				t.Errorf("Expected IsRetryableError to return %v, got %v", tc.retryable, result)
			}
		})
	}
}
