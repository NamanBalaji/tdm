package protocol_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/protocol"
)

type mockProtocol struct {
	name           string
	canHandleURLs  []string
	initializeFunc func(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error)
	createConnFunc func(urlStr string, chunk *chunk.Chunk, config *common.Config) (connection.Connection, error)
	updateConnFunc func(conn connection.Connection, chunk *chunk.Chunk)
	chunkMgrFunc   func(downloadID uuid.UUID, tempDir string) (chunk.Manager, error)
}

func (m *mockProtocol) CanHandle(url string) bool {
	for _, u := range m.canHandleURLs {
		if u == url {
			return true
		}
	}
	return false
}

func (m *mockProtocol) Initialize(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error) {
	if m.initializeFunc != nil {
		return m.initializeFunc(ctx, url, config)
	}
	return &common.DownloadInfo{
		URL:            url,
		Filename:       fmt.Sprintf("%s-file.dat", m.name),
		TotalSize:      1000,
		SupportsRanges: true,
	}, nil
}

func (m *mockProtocol) CreateConnection(urlStr string, chunk *chunk.Chunk, config *common.Config) (connection.Connection, error) {
	if m.createConnFunc != nil {
		return m.createConnFunc(urlStr, chunk, config)
	}
	return &mockConnection{url: urlStr}, nil
}

func (m *mockProtocol) UpdateConnection(conn connection.Connection, chunk *chunk.Chunk) {
	if m.updateConnFunc != nil {
		m.updateConnFunc(conn, chunk)
	}
}

func (m *mockProtocol) GetChunkManager(downloadID uuid.UUID, tempDir string) (chunk.Manager, error) {
	if m.chunkMgrFunc != nil {
		return m.chunkMgrFunc(downloadID, tempDir)
	}
	return &mockChunkManager{}, nil
}

type mockConnection struct {
	url     string
	headers map[string]string
	alive   bool
}

func (m *mockConnection) Connect(ctx context.Context) error               { m.alive = true; return nil }
func (m *mockConnection) Read(ctx context.Context, p []byte) (int, error) { return 0, nil }
func (m *mockConnection) Close() error                                    { m.alive = false; return nil }
func (m *mockConnection) IsAlive() bool                                   { return m.alive }
func (m *mockConnection) Reset(ctx context.Context) error                 { return m.Connect(ctx) }
func (m *mockConnection) GetURL() string                                  { return m.url }
func (m *mockConnection) GetHeaders() map[string]string                   { return m.headers }
func (m *mockConnection) SetHeader(k, v string) {
	if m.headers == nil {
		m.headers = make(map[string]string)
	}
	m.headers[k] = v
}
func (m *mockConnection) SetTimeout(timeout time.Duration) {}

type mockChunkManager struct{}

func (m *mockChunkManager) CreateChunks(downloadID uuid.UUID, info *common.DownloadInfo, config *common.Config, progressFn func(int64)) ([]*chunk.Chunk, error) {
	return nil, nil
}
func (m *mockChunkManager) MergeChunks(chunks []*chunk.Chunk, targetPath string) error { return nil }
func (m *mockChunkManager) CleanupChunks(chunks []*chunk.Chunk) error                  { return nil }

// Helper function to create test chunks
func createTestChunk(t *testing.T) *chunk.Chunk {
	t.Helper()
	downloadID := uuid.New()
	return &chunk.Chunk{
		ID:         uuid.New(),
		DownloadID: downloadID,
		StartByte:  0,
		EndByte:    999,
		Status:     common.StatusPending,
	}
}

func TestHandler_RegisterProtocol(t *testing.T) {
	tests := []struct {
		name      string
		protocols []*mockProtocol
		testURL   string
		wantErr   bool
	}{
		{
			name: "register single custom protocol",
			protocols: []*mockProtocol{
				{
					name:          "custom",
					canHandleURLs: []string{"custom://example.com"},
				},
			},
			testURL: "custom://example.com",
			wantErr: false,
		},
		{
			name: "register multiple protocols",
			protocols: []*mockProtocol{
				{
					name:          "protocol1",
					canHandleURLs: []string{"proto1://example.com"},
				},
				{
					name:          "protocol2",
					canHandleURLs: []string{"proto2://example.com"},
				},
			},
			testURL: "proto2://example.com",
			wantErr: false,
		},
		{
			name: "register overlapping protocols - first one wins",
			protocols: []*mockProtocol{
				{
					name:          "first",
					canHandleURLs: []string{"shared://example.com"},
				},
				{
					name:          "second",
					canHandleURLs: []string{"shared://example.com"},
				},
			},
			testURL: "shared://example.com",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()

			// Register all protocols
			for _, p := range tt.protocols {
				h.RegisterProtocol(p)
			}

			// Test that the protocol was registered
			handler, err := h.GetHandler(tt.testURL)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if handler == nil {
				t.Error("Expected handler but got nil")
				return
			}

			// For overlapping protocols test, verify first one is returned
			if tt.name == "register overlapping protocols - first one wins" {
				mockHandler := handler.(*mockProtocol)
				if mockHandler.name != "first" {
					t.Errorf("Expected first protocol to be returned, got %s", mockHandler.name)
				}
			}
		})
	}
}

func TestHandler_Initialize(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		config     *common.Config
		protocol   *mockProtocol
		wantErr    bool
		wantErrIs  error
		wantResult *common.DownloadInfo
	}{
		{
			name: "successful initialization",
			url:  "custom://example.com/file",
			config: &common.Config{
				Directory: "/downloads",
			},
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com/file"},
			},
			wantErr: false,
			wantResult: &common.DownloadInfo{
				URL:            "custom://example.com/file",
				Filename:       "custom-file.dat",
				TotalSize:      1000,
				SupportsRanges: true,
			},
		},
		{
			name:      "empty URL",
			url:       "",
			config:    nil,
			protocol:  nil,
			wantErr:   true,
			wantErrIs: protocol.ErrInvalidURL,
		},
		{
			name:      "unsupported protocol",
			url:       "unknown://example.com",
			config:    nil,
			protocol:  nil,
			wantErr:   true,
			wantErrIs: protocol.ErrUnsupportedProtocol,
		},
		{
			name:   "protocol initialization error",
			url:    "custom://example.com",
			config: nil,
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
				initializeFunc: func(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error) {
					return nil, errors.New("initialization failed")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()

			if tt.protocol != nil {
				h.RegisterProtocol(tt.protocol)
			}

			ctx := context.Background()
			result, err := h.Initialize(ctx, tt.url, tt.config)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("Expected error %v, got %v", tt.wantErrIs, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected result but got nil")
				return
			}

			if tt.wantResult != nil {
				if result.URL != tt.wantResult.URL {
					t.Errorf("Expected URL %s, got %s", tt.wantResult.URL, result.URL)
				}
				if result.Filename != tt.wantResult.Filename {
					t.Errorf("Expected filename %s, got %s", tt.wantResult.Filename, result.Filename)
				}
				if result.TotalSize != tt.wantResult.TotalSize {
					t.Errorf("Expected total size %d, got %d", tt.wantResult.TotalSize, result.TotalSize)
				}
			}
		})
	}
}

func TestHandler_GetHandler(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		protocols []*mockProtocol
		wantErr   bool
		wantErrIs error
		wantName  string
	}{
		{
			name: "get handler for registered protocol",
			url:  "custom://example.com",
			protocols: []*mockProtocol{
				{
					name:          "custom",
					canHandleURLs: []string{"custom://example.com"},
				},
			},
			wantErr:  false,
			wantName: "custom",
		},
		{
			name:      "empty URL",
			url:       "",
			protocols: nil,
			wantErr:   true,
			wantErrIs: protocol.ErrInvalidURL,
		},
		{
			name:      "unsupported protocol",
			url:       "unknown://example.com",
			protocols: nil,
			wantErr:   true,
			wantErrIs: protocol.ErrUnsupportedProtocol,
		},
		{
			name: "multiple protocols - first match wins",
			url:  "multi://example.com",
			protocols: []*mockProtocol{
				{
					name:          "first",
					canHandleURLs: []string{"multi://example.com"},
				},
				{
					name:          "second",
					canHandleURLs: []string{"multi://example.com"},
				},
			},
			wantErr:  false,
			wantName: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()

			for _, p := range tt.protocols {
				h.RegisterProtocol(p)
			}

			handler, err := h.GetHandler(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("Expected error %v, got %v", tt.wantErrIs, err)
				}
				if handler != nil {
					t.Error("Expected nil handler on error")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if handler == nil {
				t.Error("Expected handler but got nil")
				return
			}

			if tt.wantName != "" {
				mockHandler := handler.(*mockProtocol)
				if mockHandler.name != tt.wantName {
					t.Errorf("Expected handler %s, got %s", tt.wantName, mockHandler.name)
				}
			}
		})
	}
}

func TestHandler_CreateConnection(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		protocol *mockProtocol
		wantErr  bool
	}{
		{
			name: "successful connection creation",
			url:  "custom://example.com",
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
			},
			wantErr: false,
		},
		{
			name: "connection creation error",
			url:  "custom://example.com",
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
				createConnFunc: func(urlStr string, chunk *chunk.Chunk, config *common.Config) (connection.Connection, error) {
					return nil, errors.New("connection failed")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()
			h.RegisterProtocol(tt.protocol)

			handler, err := h.GetHandler(tt.url)
			if err != nil {
				t.Fatalf("Failed to get handler: %v", err)
			}

			testChunk := createTestChunk(t)
			conn, err := handler.CreateConnection(tt.url, testChunk, nil)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if conn == nil {
				t.Error("Expected connection but got nil")
			}
		})
	}
}

func TestHandler_UpdateConnection(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		protocol *mockProtocol
		verify   func(t *testing.T, conn connection.Connection)
	}{
		{
			name: "successful connection update",
			url:  "custom://example.com",
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
				updateConnFunc: func(conn connection.Connection, chunk *chunk.Chunk) {
					conn.SetHeader("Updated", "true")
				},
			},
			verify: func(t *testing.T, conn connection.Connection) {
				headers := conn.GetHeaders()
				if headers["Updated"] != "true" {
					t.Error("Expected Updated header to be set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()
			h.RegisterProtocol(tt.protocol)

			handler, err := h.GetHandler(tt.url)
			if err != nil {
				t.Fatalf("Failed to get handler: %v", err)
			}

			testChunk := createTestChunk(t)
			conn, err := handler.CreateConnection(tt.url, testChunk, nil)
			if err != nil {
				t.Fatalf("Failed to create connection: %v", err)
			}

			handler.UpdateConnection(conn, testChunk)

			if tt.verify != nil {
				tt.verify(t, conn)
			}
		})
	}
}

func TestHandler_GetChunkManager(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		protocol   *mockProtocol
		downloadID uuid.UUID
		tempDir    string
		wantErr    bool
	}{
		{
			name: "successful chunk manager creation",
			url:  "custom://example.com",
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
			},
			downloadID: uuid.New(),
			tempDir:    "/tmp",
			wantErr:    false,
		},
		{
			name: "chunk manager creation error",
			url:  "custom://example.com",
			protocol: &mockProtocol{
				name:          "custom",
				canHandleURLs: []string{"custom://example.com"},
				chunkMgrFunc: func(downloadID uuid.UUID, tempDir string) (chunk.Manager, error) {
					return nil, errors.New("chunk manager failed")
				},
			},
			downloadID: uuid.New(),
			tempDir:    "/tmp",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := protocol.NewHandler()
			h.RegisterProtocol(tt.protocol)

			handler, err := h.GetHandler(tt.url)
			if err != nil {
				t.Fatalf("Failed to get handler: %v", err)
			}

			mgr, err := handler.GetChunkManager(tt.downloadID, tt.tempDir)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if mgr == nil {
				t.Error("Expected chunk manager but got nil")
			}
		})
	}
}

func TestHandler_ConcurrentAccess(t *testing.T) {
	h := protocol.NewHandler()

	customProtocol := &mockProtocol{
		name:          "concurrent",
		canHandleURLs: []string{"concurrent://example.com"},
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	// Test concurrent registration and access
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			if index%2 == 0 {
				// Register protocol
				protocol := &mockProtocol{
					name:          fmt.Sprintf("protocol-%d", index),
					canHandleURLs: []string{fmt.Sprintf("test-%d://example.com", index)},
				}
				h.RegisterProtocol(protocol)
			} else {
				// Access existing protocol
				if index == 1 {
					// First access, register the base protocol
					h.RegisterProtocol(customProtocol)
				}
				_, err := h.GetHandler("concurrent://example.com")
				if err != nil && !errors.Is(err, protocol.ErrUnsupportedProtocol) {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any unexpected errors
	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestHandler_ErrorConditions(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *protocol.Handler
		testFunc  func(*protocol.Handler) error
		wantErr   bool
		wantErrIs error
	}{
		{
			name: "nil protocol registration",
			setupFunc: func() *protocol.Handler {
				return protocol.NewHandler()
			},
			testFunc: func(h *protocol.Handler) error {
				h.RegisterProtocol(nil)
				return nil
			},
			wantErr: false, // Registration should not panic
		},
		{
			name: "initialize with nil context",
			setupFunc: func() *protocol.Handler {
				h := protocol.NewHandler()
				h.RegisterProtocol(&mockProtocol{
					name:          "test",
					canHandleURLs: []string{"test://example.com"},
				})
				return h
			},
			testFunc: func(h *protocol.Handler) error {
				_, err := h.Initialize(nil, "test://example.com", nil)
				return err
			},
			wantErr: false, // Should handle nil context gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.setupFunc()
			err := tt.testFunc(h)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("Expected error %v, got %v", tt.wantErrIs, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
