package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	httpHandler "github.com/NamanBalaji/tdm/internal/http"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func createTestChunk(t *testing.T, startByte, endByte, downloaded int64) *chunk.Chunk {
	t.Helper()

	downloadID := uuid.New()
	c := chunk.NewChunk(downloadID, startByte, endByte, nil)

	if downloaded > 0 {
		c.SetDownloaded(downloaded)
	}

	tempDir := t.TempDir()
	c.TempFilePath = filepath.Join(tempDir, c.ID.String())

	f, err := os.Create(c.TempFilePath)
	if err != nil {
		t.Fatalf("Failed to create temp file for chunk: %v", err)
	}
	f.Close()

	return c
}

func TestNewHandler(t *testing.T) {
	h := httpHandler.NewHandler()
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestCanHandle(t *testing.T) {
	h := httpHandler.NewHandler()

	tests := []struct {
		name     string
		setup    func() *httptest.Server
		url      string
		expected bool
	}{
		{
			name: "downloadable HTTP URL",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/pdf")
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: true,
		},
		{
			name: "non-downloadable HTML URL",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusOK)
				}))
			},
			expected: false,
		},
		{
			name:     "invalid URL",
			url:      "invalid-url",
			expected: false,
		},
		{
			name:     "empty URL",
			url:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var url string
			if tt.setup != nil {
				server := tt.setup()
				defer server.Close()
				url = server.URL
			} else {
				url = tt.url
			}

			result := h.CanHandle(url)
			if result != tt.expected {
				t.Errorf("CanHandle(%s) = %v, expected %v", url, result, tt.expected)
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	h := httpHandler.NewHandler()

	tests := []struct {
		name        string
		setup       func() *httptest.Server
		config      *common.Config
		expectError bool
		checkResult func(*testing.T, *common.DownloadInfo)
	}{
		{
			name: "successful HEAD request",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Set("Accept-Ranges", "bytes")
						w.Header().Set("Content-Type", "application/pdf")
						w.Header().Set("Content-Length", "1000")
						w.Header().Set("Content-Disposition", "attachment; filename=test.pdf")
						w.WriteHeader(http.StatusOK)
						return
					}
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}))
			},
			config: &common.Config{
				Headers: map[string]string{"Authorization": "Bearer token"},
			},
			expectError: false,
			checkResult: func(t *testing.T, info *common.DownloadInfo) {
				if info.TotalSize != 1000 {
					t.Errorf("Expected TotalSize=1000, got %d", info.TotalSize)
				}
				if !info.SupportsRanges {
					t.Error("Expected SupportsRanges=true")
				}
				if info.Filename != "test.pdf" {
					t.Errorf("Expected Filename=test.pdf, got %s", info.Filename)
				}
				if info.MimeType != "application/pdf" {
					t.Errorf("Expected MimeType=application/pdf, got %s", info.MimeType)
				}
			},
		},
		{
			name: "HEAD fails, fallback to Range GET",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusMethodNotAllowed)
						return
					}
					if r.Method == http.MethodGet && r.Header.Get("Range") == "bytes=0-0" {
						w.Header().Set("Content-Range", "bytes 0-0/2000")
						w.Header().Set("Content-Type", "application/octet-stream")
						w.WriteHeader(http.StatusPartialContent)
						w.Write([]byte("a"))
						return
					}
					http.Error(w, "Unexpected request", http.StatusBadRequest)
				}))
			},
			config:      &common.Config{},
			expectError: false,
			checkResult: func(t *testing.T, info *common.DownloadInfo) {
				if info.TotalSize != 2000 {
					t.Errorf("Expected TotalSize=2000, got %d", info.TotalSize)
				}
				if !info.SupportsRanges {
					t.Error("Expected SupportsRanges=true")
				}
			},
		},
		{
			name: "HEAD and Range fail, fallback to regular GET",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.WriteHeader(http.StatusMethodNotAllowed)
						return
					}
					if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
						w.WriteHeader(http.StatusOK) // Should be 206 for ranges
						w.Write([]byte("Full content"))
						return
					}
					if r.Method == http.MethodGet {
						w.Header().Set("Content-Type", "application/pdf")
						w.Header().Set("Content-Length", "3000")
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("Full content"))
						return
					}
					http.Error(w, "Unexpected request", http.StatusBadRequest)
				}))
			},
			config:      &common.Config{}, // Use empty config instead of nil
			expectError: false,
			checkResult: func(t *testing.T, info *common.DownloadInfo) {
				if info.SupportsRanges {
					t.Error("Expected SupportsRanges=false for regular GET")
				}
				if info.TotalSize != 3000 {
					t.Errorf("Expected TotalSize=3000, got %d", info.TotalSize)
				}
			},
		},
		{
			name: "all methods fail",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			config:      &common.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setup()
			defer server.Close()

			ctx := context.Background()
			info, err := h.Initialize(ctx, server.URL, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if info == nil {
				t.Fatal("Expected non-nil DownloadInfo")
			}

			if info.URL != server.URL {
				t.Errorf("Expected URL=%s, got %s", server.URL, info.URL)
			}

			if tt.checkResult != nil {
				tt.checkResult(t, info)
			}
		})
	}
}

func TestCreateConnection(t *testing.T) {
	h := httpHandler.NewHandler()

	tests := []struct {
		name        string
		chunk       *chunk.Chunk
		config      *common.Config
		expectError bool
		checkConn   func(*testing.T, *chunk.Chunk, interface{})
	}{
		{
			name:        "basic connection creation",
			chunk:       createTestChunk(t, 0, 999, 0),
			config:      &common.Config{},
			expectError: false,
			checkConn: func(t *testing.T, c *chunk.Chunk, conn interface{}) {
				headers := conn.(interface{ GetHeaders() map[string]string }).GetHeaders()
				expectedRange := "bytes=0-999"
				if headers["Range"] != expectedRange {
					t.Errorf("Expected Range header=%s, got %s", expectedRange, headers["Range"])
				}
				if headers["User-Agent"] != httpPkg.DefaultUserAgent {
					t.Errorf("Expected User-Agent=%s, got %s", httpPkg.DefaultUserAgent, headers["User-Agent"])
				}
			},
		},
		{
			name:  "connection with partially downloaded chunk",
			chunk: createTestChunk(t, 0, 999, 200),
			config: &common.Config{
				Headers: map[string]string{
					"Authorization": "Bearer token123",
					"Custom-Header": "custom-value",
				},
			},
			expectError: false,
			checkConn: func(t *testing.T, c *chunk.Chunk, conn interface{}) {
				headers := conn.(interface{ GetHeaders() map[string]string }).GetHeaders()
				expectedRange := "bytes=200-999"
				if headers["Range"] != expectedRange {
					t.Errorf("Expected Range header=%s, got %s", expectedRange, headers["Range"])
				}
				if headers["Authorization"] != "Bearer token123" {
					t.Errorf("Expected Authorization header=Bearer token123, got %s", headers["Authorization"])
				}
				if headers["Custom-Header"] != "custom-value" {
					t.Errorf("Expected Custom-Header=custom-value, got %s", headers["Custom-Header"])
				}
			},
		},
		{
			name:        "connection with nil config",
			chunk:       createTestChunk(t, 100, 199, 0),
			config:      nil,
			expectError: false,
			checkConn: func(t *testing.T, c *chunk.Chunk, conn interface{}) {
				headers := conn.(interface{ GetHeaders() map[string]string }).GetHeaders()
				expectedRange := "bytes=100-199"
				if headers["Range"] != expectedRange {
					t.Errorf("Expected Range header=%s, got %s", expectedRange, headers["Range"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := h.CreateConnection("http://example.com/file", tt.chunk, tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if conn == nil {
				t.Fatal("Expected non-nil connection")
			}

			if tt.checkConn != nil {
				tt.checkConn(t, tt.chunk, conn)
			}
		})
	}
}

func TestUpdateConnection(t *testing.T) {
	h := httpHandler.NewHandler()

	tests := []struct {
		name        string
		chunk       *chunk.Chunk
		expectRange string
	}{
		{
			name:        "update connection for fresh chunk",
			chunk:       createTestChunk(t, 0, 999, 0),
			expectRange: "bytes=0-999",
		},
		{
			name:        "update connection for partially downloaded chunk",
			chunk:       createTestChunk(t, 0, 999, 500),
			expectRange: "bytes=500-999",
		},
		{
			name:        "update connection for different range",
			chunk:       createTestChunk(t, 1000, 1999, 250),
			expectRange: "bytes=1250-1999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockConnection{
				headers: make(map[string]string),
			}

			h.UpdateConnection(mockConn, tt.chunk)

			if mockConn.headers["Range"] != tt.expectRange {
				t.Errorf("Expected Range header=%s, got %s", tt.expectRange, mockConn.headers["Range"])
			}
		})
	}
}

func TestGetChunkManager(t *testing.T) {
	h := httpHandler.NewHandler()
	tempDir := t.TempDir()
	downloadID := uuid.New()

	manager, err := h.GetChunkManager(downloadID, tempDir)
	if err != nil {
		t.Fatalf("GetChunkManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("Expected non-nil chunk manager")
	}
}

func TestInitializeTimeout(t *testing.T) {
	h := httpHandler.NewHandler()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := h.Initialize(ctx, server.URL, &common.Config{})
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}

func TestInitializeWithInvalidContentRange(t *testing.T) {
	h := httpHandler.NewHandler()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-0/not-a-number")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
			return
		}
		if r.Method == http.MethodGet {
			// Also fail the regular GET fallback
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := h.Initialize(ctx, server.URL, &common.Config{})
	if err == nil {
		t.Error("Expected error for invalid content range, got nil")
	}
}

type mockConnection struct {
	headers map[string]string
}

func (m *mockConnection) SetHeader(key, value string) {
	m.headers[key] = value
}

func (m *mockConnection) GetHeaders() map[string]string {
	return m.headers
}

func (m *mockConnection) Connect(ctx context.Context) error                     { return nil }
func (m *mockConnection) Read(ctx context.Context, p []byte) (n int, err error) { return 0, nil }
func (m *mockConnection) Close() error                                          { return nil }
func (m *mockConnection) IsAlive() bool                                         { return true }
func (m *mockConnection) Reset(ctx context.Context) error                       { return nil }
func (m *mockConnection) GetURL() string                                        { return "http://test.com" }
func (m *mockConnection) SetTimeout(timeout time.Duration)                      {}
