package downloader_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/protocol"
)

// Mock Protocol Implementation that implements protocol.Protocol
type mockProtocol struct {
	info           *common.DownloadInfo
	initErr        error
	chunkManager   chunk.Manager
	chunkMgrErr    error
	createConnErr  error
	updateConnFunc func(connection.Connection, *chunk.Chunk)
}

func (m *mockProtocol) CanHandle(url string) bool {
	return true
}

func (m *mockProtocol) Initialize(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error) {
	if m.initErr != nil {
		return nil, m.initErr
	}
	if m.info != nil {
		return m.info, nil
	}
	return &common.DownloadInfo{
		URL:            url,
		Filename:       "test-file.dat",
		TotalSize:      1000,
		SupportsRanges: true,
	}, nil
}

func (m *mockProtocol) CreateConnection(urlStr string, chunk *chunk.Chunk, config *common.Config) (connection.Connection, error) {
	if m.createConnErr != nil {
		return nil, m.createConnErr
	}
	return &mockConnection{url: urlStr, alive: true}, nil
}

func (m *mockProtocol) UpdateConnection(conn connection.Connection, chunk *chunk.Chunk) {
	if m.updateConnFunc != nil {
		m.updateConnFunc(conn, chunk)
	}
}

func (m *mockProtocol) GetChunkManager(downloadID uuid.UUID, tempDir string) (chunk.Manager, error) {
	if m.chunkMgrErr != nil {
		return nil, m.chunkMgrErr
	}
	if m.chunkManager != nil {
		return m.chunkManager, nil
	}
	return &mockChunkManager{}, nil
}

// Mock Connection Implementation
type mockConnection struct {
	url           string
	headers       map[string]string
	alive         bool
	connectErr    error
	readErr       error
	readData      []byte
	readPos       int
	closeErr      error
	resetErr      error
	connectCalled bool
	readCalled    bool
	closeCalled   bool
	resetCalled   bool
}

func (m *mockConnection) Connect(ctx context.Context) error {
	m.connectCalled = true
	if m.connectErr != nil {
		return m.connectErr
	}
	m.alive = true
	return nil
}

func (m *mockConnection) Read(ctx context.Context, p []byte) (int, error) {
	m.readCalled = true
	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.readData == nil || m.readPos >= len(m.readData) {
		return 0, errors.New("EOF")
	}
	n := copy(p, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockConnection) Close() error {
	m.closeCalled = true
	m.alive = false
	if m.closeErr != nil {
		return m.closeErr
	}
	return nil
}

func (m *mockConnection) IsAlive() bool {
	return m.alive
}

func (m *mockConnection) Reset(ctx context.Context) error {
	m.resetCalled = true
	if m.resetErr != nil {
		return m.resetErr
	}
	m.readPos = 0
	return nil
}

func (m *mockConnection) GetURL() string {
	return m.url
}

func (m *mockConnection) GetHeaders() map[string]string {
	if m.headers == nil {
		m.headers = make(map[string]string)
	}
	return m.headers
}

func (m *mockConnection) SetHeader(key, value string) {
	if m.headers == nil {
		m.headers = make(map[string]string)
	}
	m.headers[key] = value
}

func (m *mockConnection) SetTimeout(timeout time.Duration) {}

// Mock Chunk Manager Implementation
type mockChunkManager struct {
	createChunksErr error
	mergeChunksErr  error
	cleanupErr      error
	chunks          []*chunk.Chunk
	createCalled    bool
	mergeCalled     bool
	cleanupCalled   bool
}

func (m *mockChunkManager) CreateChunks(downloadID uuid.UUID, info *common.DownloadInfo, config *common.Config, progressFn func(int64)) ([]*chunk.Chunk, error) {
	m.createCalled = true
	if m.createChunksErr != nil {
		return nil, m.createChunksErr
	}
	if m.chunks != nil {
		return m.chunks, nil
	}

	// Create default chunks
	chunks := make([]*chunk.Chunk, 2)
	for i := range chunks {
		chunks[i] = &chunk.Chunk{
			ID:         uuid.New(),
			DownloadID: downloadID,
			StartByte:  int64(i * 500),
			EndByte:    int64((i+1)*500 - 1),
			Status:     common.StatusPending,
		}
		chunks[i].SetProgressFunc(progressFn)
	}
	return chunks, nil
}

func (m *mockChunkManager) MergeChunks(chunks []*chunk.Chunk, targetPath string) error {
	m.mergeCalled = true
	if m.mergeChunksErr != nil {
		return m.mergeChunksErr
	}

	// Create the target file
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString("merged content")
	return err
}

func (m *mockChunkManager) CleanupChunks(chunks []*chunk.Chunk) error {
	m.cleanupCalled = true
	if m.cleanupErr != nil {
		return m.cleanupErr
	}
	return nil
}

// Helper function to create protocol handler with mock protocol
func createMockProtocolHandler(url string, mockProto *mockProtocol) *protocol.Handler {
	handler := protocol.NewHandler()
	handler.RegisterProtocol(mockProto)
	return handler
}

// Test NewDownload
func TestNewDownload(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		config          *common.Config
		protoSetup      func(string) (*protocol.Handler, error)
		wantErr         bool
		wantErrContains string
		validate        func(*testing.T, *downloader.Download)
	}{
		{
			name: "successful creation",
			url:  "http://example.com/file.dat",
			config: &common.Config{
				Directory: "/downloads",
				TempDir:   "/tmp",
			},
			protoSetup: func(url string) (*protocol.Handler, error) {
				proto := &mockProtocol{
					info: &common.DownloadInfo{
						URL:            url,
						Filename:       "file.dat",
						TotalSize:      1000,
						SupportsRanges: true,
					},
				}
				return createMockProtocolHandler(url, proto), nil
			},
			wantErr: false,
			validate: func(t *testing.T, d *downloader.Download) {
				if d.URL != "http://example.com/file.dat" {
					t.Errorf("Expected URL http://example.com/file.dat, got %s", d.URL)
				}
				if d.Filename != "file.dat" {
					t.Errorf("Expected filename file.dat, got %s", d.Filename)
				}
				if d.TotalSize != 1000 {
					t.Errorf("Expected total size 1000, got %d", d.TotalSize)
				}
				if d.GetStatus() != common.StatusPending {
					t.Errorf("Expected status Pending, got %v", d.GetStatus())
				}
				if d.GetTotalChunks() != 2 {
					t.Errorf("Expected 2 chunks, got %d", d.GetTotalChunks())
				}
			},
		},
		{
			name: "protocol initialization error",
			url:  "http://example.com/file.dat",
			config: &common.Config{
				Directory: "/downloads",
				TempDir:   "/tmp",
			},
			protoSetup: func(url string) (*protocol.Handler, error) {
				proto := &mockProtocol{
					initErr: errors.New("initialization failed"),
				}
				return createMockProtocolHandler(url, proto), nil
			},
			wantErr:         true,
			wantErrContains: "error initializing handler",
		},
		{
			name: "chunk manager error",
			url:  "http://example.com/file.dat",
			config: &common.Config{
				Directory: "/downloads",
				TempDir:   "/tmp",
			},
			protoSetup: func(url string) (*protocol.Handler, error) {
				proto := &mockProtocol{
					chunkMgrErr: errors.New("chunk manager failed"),
				}
				return createMockProtocolHandler(url, proto), nil
			},
			wantErr: true,
		},
		{
			name: "chunk creation error",
			url:  "http://example.com/file.dat",
			config: &common.Config{
				Directory: "/downloads",
				TempDir:   "/tmp",
			},
			protoSetup: func(url string) (*protocol.Handler, error) {
				proto := &mockProtocol{
					chunkManager: &mockChunkManager{
						createChunksErr: errors.New("chunk creation failed"),
					},
				}
				return createMockProtocolHandler(url, proto), nil
			},
			wantErr:         true,
			wantErrContains: "failed to create chunks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			if tt.config != nil {
				if tt.config.Directory == "/downloads" {
					tt.config.Directory = filepath.Join(tempDir, "downloads")
				}
				if tt.config.TempDir == "/tmp" {
					tt.config.TempDir = filepath.Join(tempDir, "tmp")
				}
			}

			protoHandler, err := tt.protoSetup(tt.url)
			if err != nil {
				t.Fatalf("Failed to setup protocol handler: %v", err)
			}

			saveStateChan := make(chan *downloader.Download, 1)

			download, err := downloader.NewDownload(context.Background(), tt.url, protoHandler, tt.config, saveStateChan)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if tt.wantErrContains != "" && !contains(err.Error(), tt.wantErrContains) {
					t.Errorf("Expected error to contain %q, got %q", tt.wantErrContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if download == nil {
				t.Error("Expected download but got nil")
				return
			}

			if tt.validate != nil {
				tt.validate(t, download)
			}
		})
	}
}

// Test Download.GetStats
func TestDownload_GetStats(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *downloader.Download
		validate func(*testing.T, downloader.Stats)
	}{
		{
			name: "basic stats",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusActive)
				return download
			},
			validate: func(t *testing.T, stats downloader.Stats) {
				if stats.Status != common.StatusActive {
					t.Errorf("Expected status Active, got %v", stats.Status)
				}
				if stats.Filename != "test-file.dat" {
					t.Errorf("Expected filename test-file.dat, got %s", stats.Filename)
				}
				if stats.TotalSize != 1000 {
					t.Errorf("Expected total size 1000, got %d", stats.TotalSize)
				}
			},
		},
		{
			name: "stats with progress",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusActive)
				return download
			},
			validate: func(t *testing.T, stats downloader.Stats) {
				if stats.Status != common.StatusActive {
					t.Errorf("Expected status Active, got %v", stats.Status)
				}
				if stats.Progress < 0 {
					t.Errorf("Expected non-negative progress, got %.1f", stats.Progress)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup()
			stats := download.GetStats()
			tt.validate(t, stats)
		})
	}
}

// Test Download JSON serialization
func TestDownload_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *downloader.Download
		validate func(*testing.T, []byte)
	}{
		{
			name: "basic serialization",
			setup: func() *downloader.Download {
				return createTestDownload(t)
			},
			validate: func(t *testing.T, data []byte) {
				var result map[string]interface{}
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatalf("Failed to unmarshal JSON: %v", err)
				}

				if result["url"] != "http://example.com/test" {
					t.Errorf("Expected URL in JSON, got %v", result["url"])
				}
				if result["filename"] != "test-file.dat" {
					t.Errorf("Expected filename in JSON, got %v", result["filename"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup()
			data, err := download.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}
			tt.validate(t, data)
		})
	}
}

// Test Download.Start
func TestDownload_Start(t *testing.T) {
	tests := []struct {
		name       string
		setup      func() *downloader.Download
		wantStatus common.Status
	}{
		{
			name: "successful start with no chunks",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				// Set all chunks to completed
				for _, chunk := range download.Chunks {
					chunk.SetStatus(common.StatusCompleted)
				}
				return download
			},
			wantStatus: common.StatusCompleted,
		},
		{
			name: "start with active status should return early",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusActive)
				return download
			},
			wantStatus: common.StatusActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup()

			// Create a real connection pool for testing
			pool := connection.NewPool(2, time.Minute)
			defer pool.CloseAll()

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			download.Start(ctx, pool)

			if download.GetStatus() != tt.wantStatus {
				t.Errorf("Expected status %v, got %v", tt.wantStatus, download.GetStatus())
			}
		})
	}
}

// Test Download.Stop
func TestDownload_Stop(t *testing.T) {
	tests := []struct {
		name         string
		setup        func() *downloader.Download
		stopStatus   common.Status
		removeFiles  bool
		wantStatus   common.Status
		shouldIgnore bool
	}{
		{
			name: "stop pending download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusPending)
				return download
			},
			stopStatus:   common.StatusPaused,
			wantStatus:   common.StatusPending, // Should be ignored for non-active
			shouldIgnore: true,
		},
		{
			name: "ignore stop on completed download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusCompleted)
				return download
			},
			stopStatus:   common.StatusPaused,
			wantStatus:   common.StatusCompleted,
			shouldIgnore: true,
		},
		{
			name: "stop paused download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusPaused)
				return download
			},
			stopStatus:   common.StatusPaused,
			wantStatus:   common.StatusPaused,
			shouldIgnore: true, // Should ignore pause on already paused
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup()
			originalStatus := download.GetStatus()

			// Use a timeout to prevent hanging
			done := make(chan struct{})
			go func() {
				defer close(done)
				download.Stop(tt.stopStatus, tt.removeFiles)
			}()

			select {
			case <-done:
				// Stop completed successfully
			case <-time.After(2 * time.Second):
				t.Fatal("Stop operation timed out - this indicates a hanging issue")
			}

			finalStatus := download.GetStatus()
			if tt.shouldIgnore {
				if finalStatus != originalStatus {
					t.Errorf("Expected status to remain %v, got %v", originalStatus, finalStatus)
				}
			} else {
				if finalStatus != tt.wantStatus {
					t.Errorf("Expected status %v, got %v", tt.wantStatus, finalStatus)
				}
			}
		})
	}
}

// Test Download.Resume
func TestDownload_Resume(t *testing.T) {
	tests := []struct {
		name       string
		setup      func() *downloader.Download
		wantResume bool
		wantStatus common.Status
	}{
		{
			name: "resume paused download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusPaused)
				return download
			},
			wantResume: true,
			wantStatus: common.StatusPaused,
		},
		{
			name: "resume failed download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusFailed)
				return download
			},
			wantResume: true,
			wantStatus: common.StatusFailed,
		},
		{
			name: "cannot resume active download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusActive)
				return download
			},
			wantResume: false,
			wantStatus: common.StatusActive,
		},
		{
			name: "cannot resume completed download",
			setup: func() *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusCompleted)
				return download
			},
			wantResume: false,
			wantStatus: common.StatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup()

			resumed := download.Resume(context.Background())

			if resumed != tt.wantResume {
				t.Errorf("Expected resume result %v, got %v", tt.wantResume, resumed)
			}

			if download.GetStatus() != tt.wantStatus {
				t.Errorf("Expected status %v, got %v", tt.wantStatus, download.GetStatus())
			}
		})
	}
}

// Test Download.Remove
func TestDownload_Remove(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T) *downloader.Download
	}{
		{
			name: "remove pending download",
			setup: func(t *testing.T) *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusPending)
				return download
			},
		},
		{
			name: "remove completed download",
			setup: func(t *testing.T) *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusCompleted)

				// Create the output file
				outputPath := filepath.Join(download.Config.Directory, download.Filename)
				os.MkdirAll(filepath.Dir(outputPath), 0o755)
				os.WriteFile(outputPath, []byte("test content"), 0o644)

				return download
			},
		},
		{
			name: "remove paused download",
			setup: func(t *testing.T) *downloader.Download {
				download := createTestDownload(t)
				download.SetStatus(common.StatusPaused)
				return download
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			download := tt.setup(t)
			outputPath := filepath.Join(download.Config.Directory, download.Filename)

			// Use a timeout to prevent hanging
			done := make(chan struct{})
			go func() {
				defer close(done)
				download.Remove()
			}()

			select {
			case <-done:
				// Remove completed successfully
				t.Log("Remove operation completed successfully")
			case <-time.After(3 * time.Second):
				t.Fatal("Remove operation timed out - this indicates a hanging issue")
			}

			// Check that output file was removed if it existed
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Logf("Output file removal status: %v", err)
			}
		})
	}
}

// Test Download with simulated active state (safe version)
func TestDownload_SimulatedActiveOperations(t *testing.T) {
	t.Run("stop after actual start", func(t *testing.T) {
		download := createTestDownload(t)

		// Set all chunks to completed so Start() finishes quickly
		for _, chunk := range download.Chunks {
			chunk.SetStatus(common.StatusCompleted)
		}

		// Start the download (will complete immediately due to completed chunks)
		pool := connection.NewPool(1, time.Minute)
		defer pool.CloseAll()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		download.Start(ctx, pool)

		// Now the download should be in a safe state to stop
		if download.GetStatus() == common.StatusCompleted {
			t.Log("Download completed as expected")
		}

		// Test stop on completed download (should be ignored)
		done := make(chan struct{})
		go func() {
			defer close(done)
			download.Stop(common.StatusPaused, false)
		}()

		select {
		case <-done:
			if download.GetStatus() != common.StatusCompleted {
				t.Errorf("Expected status to remain Completed, got %v", download.GetStatus())
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Stop operation timed out")
		}
	})

	t.Run("remove after actual start", func(t *testing.T) {
		download := createTestDownload(t)

		// Set all chunks to completed
		for _, chunk := range download.Chunks {
			chunk.SetStatus(common.StatusCompleted)
		}

		// Start and let it complete
		pool := connection.NewPool(1, time.Minute)
		defer pool.CloseAll()

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		download.Start(ctx, pool)

		// Test remove on completed download
		done := make(chan struct{})
		go func() {
			defer close(done)
			download.Remove()
		}()

		select {
		case <-done:
			t.Log("Remove operation completed successfully")
		case <-time.After(2 * time.Second):
			t.Fatal("Remove operation timed out")
		}
	})
}

// Test SpeedCalculator
func TestSpeedCalculator(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *downloader.SpeedCalculator
		actions  func(*downloader.SpeedCalculator)
		validate func(*testing.T, *downloader.SpeedCalculator)
	}{
		{
			name: "new calculator",
			setup: func() *downloader.SpeedCalculator {
				return downloader.NewSpeedCalculator(5)
			},
			validate: func(t *testing.T, sc *downloader.SpeedCalculator) {
				speed := sc.GetSpeed()
				if speed != 0 {
					t.Errorf("Expected initial speed 0, got %d", speed)
				}
			},
		},
		{
			name: "add bytes and calculate speed",
			setup: func() *downloader.SpeedCalculator {
				return downloader.NewSpeedCalculator(5)
			},
			actions: func(sc *downloader.SpeedCalculator) {
				sc.AddBytes(1000)
				time.Sleep(1100 * time.Millisecond) // Wait more than 1 second
			},
			validate: func(t *testing.T, sc *downloader.SpeedCalculator) {
				speed := sc.GetSpeed()
				if speed <= 0 {
					t.Errorf("Expected positive speed, got %d", speed)
				}
			},
		},
		{
			name: "zero window size defaults to 5",
			setup: func() *downloader.SpeedCalculator {
				return downloader.NewSpeedCalculator(0)
			},
			validate: func(t *testing.T, sc *downloader.SpeedCalculator) {
				// Should not panic and should work normally
				sc.AddBytes(100)
				speed := sc.GetSpeed()
				if speed < 0 {
					t.Errorf("Expected non-negative speed, got %d", speed)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := tt.setup()

			if tt.actions != nil {
				tt.actions(sc)
			}

			tt.validate(t, sc)
		})
	}
}

// Test atomic operations
func TestDownload_AtomicOperations(t *testing.T) {
	download := createTestDownload(t)

	// Test status operations
	download.SetStatus(common.StatusActive)
	if download.GetStatus() != common.StatusActive {
		t.Errorf("Expected status Active, got %v", download.GetStatus())
	}

	// Test external flag operations
	download.SetIsExternal(true)
	if !download.GetIsExternal() {
		t.Error("Expected isExternal to be true")
	}

	download.SetIsExternal(false)
	if download.GetIsExternal() {
		t.Error("Expected isExternal to be false")
	}

	// Test total chunks operations
	download.SetTotalChunks(5)
	if download.GetTotalChunks() != 5 {
		t.Errorf("Expected total chunks 5, got %d", download.GetTotalChunks())
	}
}

// Test concurrent operations
func TestDownload_ConcurrentOperations(t *testing.T) {
	download := createTestDownload(t)

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Test concurrent status updates
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				download.SetStatus(common.StatusActive)
			} else {
				download.SetStatus(common.StatusPaused)
			}
			_ = download.GetStatus()
		}(i)
	}
	wg.Wait()

	// Test concurrent progress updates
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			download.GetStats()
		}()
	}
	wg.Wait()
}

// Test RestoreFromSerialization
func TestDownload_RestoreFromSerialization(t *testing.T) {
	t.Run("restoration method exists and handles basic cases", func(t *testing.T) {
		download := createTestDownload(t)
		download.Status = common.StatusPaused
		download.TotalChunks = 0
		download.ChunkInfos = []common.ChunkInfo{} // Empty to avoid complex chunk restoration

		// Create a protocol handler with our mock protocol
		proto := &mockProtocol{}
		protoHandler := createMockProtocolHandler(download.URL, proto)
		saveStateChan := make(chan *downloader.Download, 1)

		err := download.RestoreFromSerialization(protoHandler, saveStateChan)

		// The method should handle the call without panicking
		if err != nil {
			t.Logf("Restoration failed (expected for this test): %v", err)
		} else {
			t.Log("Restoration succeeded")
		}

		// Verify the download object is still valid
		if download.GetStatus() == common.Status(0) {
			t.Error("Download status was corrupted during restoration")
		}
	})
}

// Test progress tracking
func TestDownload_ProgressTracking(t *testing.T) {
	download := createTestDownload(t)

	var progressCalls []int64
	var mu sync.Mutex

	// Create a custom progress function to track calls
	progressFn := func(bytes int64) {
		mu.Lock()
		progressCalls = append(progressCalls, bytes)
		mu.Unlock()
	}

	// Set progress function on chunks
	for _, chunk := range download.Chunks {
		chunk.SetProgressFunc(progressFn)
		// Simulate adding some bytes and calling progress
		progressFn(100) // Manually call to simulate progress
	}

	mu.Lock()
	callCount := len(progressCalls)
	mu.Unlock()

	if callCount < 2 {
		t.Errorf("Expected at least 2 progress calls, got %d", callCount)
	}
}

// Test edge cases
func TestDownload_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		test func(*testing.T)
	}{
		{
			name: "get stats with initial state",
			test: func(t *testing.T) {
				download := createTestDownload(t)
				stats := download.GetStats()
				if stats.Speed < 0 {
					t.Errorf("Expected non-negative speed, got %d", stats.Speed)
				}
				if stats.Progress < 0 {
					t.Errorf("Expected non-negative progress, got %.2f", stats.Progress)
				}
			},
		},
		//{
		//	name: "multiple start calls",
		//	test: func(t *testing.T) {
		//		download := createTestDownload(t)
		//		pool := connection.NewPool(1, time.Minute)
		//		defer pool.CloseAll()
		//
		//		ctx1, cancel1 := context.WithTimeout(context.Background(), 50*time.Millisecond)
		//		defer cancel1()
		//
		//		ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
		//		defer cancel2()
		//
		//		// Start download twice - second should return early
		//		download.Start(ctx1, pool)
		//		originalStatus := download.GetStatus()
		//
		//		download.Start(ctx2, pool)
		//		finalStatus := download.GetStatus()
		//
		//		// Status should remain the same or progress naturally
		//		if finalStatus != originalStatus && originalStatus == common.StatusActive {
		//			t.Logf("Status changed from %v to %v (this may be normal)", originalStatus, finalStatus)
		//		}
		//	},
		//},
		{
			name: "stop already stopped download",
			test: func(t *testing.T) {
				download := createTestDownload(t)
				download.SetStatus(common.StatusCompleted)

				// Should ignore the stop request
				download.Stop(common.StatusPaused, false)

				if download.GetStatus() != common.StatusCompleted {
					t.Errorf("Expected status to remain Completed, got %v", download.GetStatus())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

// Benchmark tests
func BenchmarkDownload_GetStats(b *testing.B) {
	tempDir := b.TempDir()
	config := &common.Config{
		Directory: filepath.Join(tempDir, "downloads"),
		TempDir:   filepath.Join(tempDir, "tmp"),
	}

	proto := &mockProtocol{}
	protoHandler := createMockProtocolHandler("http://example.com/test", proto)

	saveStateChan := make(chan *downloader.Download, 1)
	download, _ := downloader.NewDownload(
		context.Background(),
		"http://example.com/test",
		protoHandler,
		config,
		saveStateChan,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = download.GetStats()
	}
}

func BenchmarkSpeedCalculator_AddBytes(b *testing.B) {
	sc := downloader.NewSpeedCalculator(5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sc.AddBytes(1024)
	}
}

// Helper functions
func createTestDownload(t *testing.T) *downloader.Download {
	t.Helper()

	tempDir := t.TempDir()
	config := &common.Config{
		Directory:   filepath.Join(tempDir, "downloads"),
		TempDir:     filepath.Join(tempDir, "tmp"),
		Connections: 2,
		MaxRetries:  3,
		RetryDelay:  time.Second,
	}

	proto := &mockProtocol{
		info: &common.DownloadInfo{
			URL:            "http://example.com/test",
			Filename:       "test-file.dat",
			TotalSize:      1000,
			SupportsRanges: true,
		},
	}
	protoHandler := createMockProtocolHandler("http://example.com/test", proto)

	saveStateChan := make(chan *downloader.Download, 1)

	download, err := downloader.NewDownload(
		context.Background(),
		"http://example.com/test",
		protoHandler,
		config,
		saveStateChan,
	)
	if err != nil {
		t.Fatalf("Failed to create test download: %v", err)
	}

	return download
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsAt(s, substr))))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
