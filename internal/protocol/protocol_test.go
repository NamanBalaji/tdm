package protocol_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/protocol"
)

type CustomProtocol struct {
	canHandleURLs []string
}

func (p *CustomProtocol) CanHandle(url string) bool {
	for _, u := range p.canHandleURLs {
		if u == url {
			return true
		}
	}
	return false
}

func (p *CustomProtocol) Initialize(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error) {
	return &common.DownloadInfo{
		URL:            url,
		Filename:       "custom-file.dat",
		MimeType:       "application/octet-stream",
		TotalSize:      1000,
		SupportsRanges: true,
	}, nil
}

func (p *CustomProtocol) CreateConnection(urlStr string, c *chunk.Chunk, downloadConfig *common.Config) (connection.Connection, error) {
	return &testConnection{
		url:     urlStr,
		headers: make(map[string]string),
	}, nil
}

func (p *CustomProtocol) UpdateConnection(conn connection.Connection, c *chunk.Chunk) {
	conn.SetHeader("X-Updated", "true")
}

type testConnection struct {
	url       string
	headers   map[string]string
	alive     bool
	data      []byte
	readPos   int
	connected bool
}

func (c *testConnection) Connect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		c.connected = true
		c.alive = true
		c.data = []byte("test data")
		c.readPos = 0
		return nil
	}
}

func (c *testConnection) Read(ctx context.Context, p []byte) (n int, err error) {
	if !c.connected {
		if err := c.Connect(ctx); err != nil {
			return 0, err
		}
	}

	if c.readPos >= len(c.data) {
		return 0, errors.New("EOF")
	}

	n = copy(p, c.data[c.readPos:])
	c.readPos += n
	return n, nil
}

func (c *testConnection) Close() error {
	c.connected = false
	c.alive = false
	return nil
}

func (c *testConnection) IsAlive() bool {
	return c.alive
}

func (c *testConnection) Reset(ctx context.Context) error {
	c.connected = false
	return c.Connect(ctx)
}

func (c *testConnection) GetURL() string {
	return c.url
}

func (c *testConnection) GetHeaders() map[string]string {
	return c.headers
}

func (c *testConnection) SetHeader(key, value string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

func (c *testConnection) SetTimeout(timeout time.Duration) {
}

func createTestChunk(t *testing.T) *chunk.Chunk {
	t.Helper()

	downloadID := uuid.New()
	chunkID := uuid.New()
	c := &chunk.Chunk{
		ID:         chunkID,
		DownloadID: downloadID,
		StartByte:  0,
		EndByte:    999,
		Status:     common.StatusPending,
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
	h := protocol.NewHandler()
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler, err := h.GetHandler(server.URL)
	if err != nil {
		t.Errorf("Expected handler for HTTP URL, got error: %v", err)
	}
	if handler == nil {
		t.Error("Expected non-nil HTTP handler")
	}
}

func TestRegisterProtocol(t *testing.T) {
	h := protocol.NewHandler()

	customProto := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
			"special://test.com",
		},
	}

	// Before registration, these URLs should return an error
	_, err := h.GetHandler("custom://example.com")
	if err == nil {
		t.Error("Expected error for unregistered protocol URL, got nil")
	}
	if !errors.Is(err, protocol.ErrUnsupportedProtocol) {
		t.Errorf("Expected ErrUnsupportedProtocol, got: %v", err)
	}

	h.RegisterProtocol(customProto)

	handler, err := h.GetHandler("custom://example.com")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if handler != customProto {
		t.Error("Expected to get our registered custom protocol")
	}

	handler, err = h.GetHandler("special://test.com")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if handler != customProto {
		t.Error("Expected to get our registered custom protocol")
	}
}

func TestGetProtocolHandler(t *testing.T) {
	h := protocol.NewHandler()

	testCases := []struct {
		name          string
		url           string
		expectError   bool
		expectedError error
	}{
		{
			name:          "Empty URL",
			url:           "",
			expectError:   true,
			expectedError: protocol.ErrInvalidURL,
		},
		{
			name:        "HTTP URL",
			url:         "http://example.com",
			expectError: false,
		},
		{
			name:          "Unsupported Protocol",
			url:           "unsupported://example.com",
			expectError:   true,
			expectedError: protocol.ErrUnsupportedProtocol,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for i, tc := range testCases {
		if tc.name == "HTTP URL" {
			testCases[i].url = server.URL
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := h.GetHandler(tc.url)

			if tc.expectError {
				if err == nil {
					t.Error("Expected an error, got nil")
					return
				}

				if tc.expectedError != nil && !errors.Is(err, tc.expectedError) {
					t.Errorf("Expected error %v, got %v", tc.expectedError, err)
				}

				if handler != nil {
					t.Errorf("Expected nil handler, got %T", handler)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}

				if handler == nil {
					t.Error("Expected a handler, got nil")
				}
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	h := protocol.NewHandler()
	ctx := t.Context()

	customProto := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
		},
	}
	h.RegisterProtocol(customProto)

	info, err := h.Initialize(ctx, "custom://example.com", nil)
	if err != nil {
		t.Errorf("Initialize returned unexpected error: %v", err)
	}

	if info == nil {
		t.Fatal("Expected non-nil DownloadInfo")
	}

	if info.URL != "custom://example.com" {
		t.Errorf("Expected URL 'custom://example.com', got %q", info.URL)
	}

	if info.Filename != "custom-file.dat" {
		t.Errorf("Expected filename 'custom-file.dat', got %q", info.Filename)
	}

	_, err = h.Initialize(ctx, "", nil)
	if err == nil || !errors.Is(err, protocol.ErrInvalidURL) {
		t.Errorf("Expected error %v for empty URL, got %v", protocol.ErrInvalidURL, err)
	}

	_, err = h.Initialize(ctx, "unsupported://example.com", nil)
	if err == nil || !errors.Is(err, protocol.ErrUnsupportedProtocol) {
		t.Errorf("Expected error %v for unsupported protocol, got %v", protocol.ErrUnsupportedProtocol, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Content-Disposition", "attachment; filename=\"test.txt\"")
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Test content"))
		}
	}))
	defer server.Close()

	info, err = h.Initialize(ctx, server.URL, nil)
	if err != nil {
		t.Errorf("Initialize with HTTP URL returned error: %v", err)
	}

	if info == nil {
		t.Fatal("Expected non-nil DownloadInfo for HTTP URL")
	}

	if info.URL != server.URL {
		t.Errorf("Expected URL %q, got %q", server.URL, info.URL)
	}
}

func TestCreateConnection(t *testing.T) {
	h := protocol.NewHandler()

	customProto := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
		},
	}
	h.RegisterProtocol(customProto)

	handler, err := h.GetHandler("custom://example.com")
	if err != nil {
		t.Fatalf("Failed to get custom protocol handler: %v", err)
	}

	testChunk := createTestChunk(t)

	conn, err := handler.CreateConnection("custom://example.com", testChunk, nil)
	if err != nil {
		t.Fatalf("CreateConnection error: %v", err)
	}

	if conn == nil {
		t.Fatal("Expected non-nil connection")
	}

	if conn.GetURL() != "custom://example.com" {
		t.Errorf("Expected URL 'custom://example.com', got %q", conn.GetURL())
	}

	config := &common.Config{
		Headers: map[string]string{
			"Custom-Header": "test-value",
		},
	}

	conn, err = handler.CreateConnection("custom://example.com", testChunk, config)
	if err != nil {
		t.Fatalf("CreateConnection with config error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	httpHandler, err := h.GetHandler(server.URL)
	if err != nil {
		t.Fatalf("Failed to get HTTP handler: %v", err)
	}

	conn, err = httpHandler.CreateConnection(server.URL, testChunk, nil)
	if err != nil {
		t.Fatalf("HTTP CreateConnection error: %v", err)
	}

	if conn == nil {
		t.Fatal("Expected non-nil HTTP connection")
	}

	if conn.GetURL() != server.URL {
		t.Errorf("Expected URL %q, got %q", server.URL, conn.GetURL())
	}
}

func TestUpdateConnection(t *testing.T) {
	h := protocol.NewHandler()

	customProto := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
		},
	}
	h.RegisterProtocol(customProto)

	handler, err := h.GetHandler("custom://example.com")
	if err != nil {
		t.Fatalf("Failed to get custom protocol handler: %v", err)
	}

	testChunk := createTestChunk(t)

	conn, err := handler.CreateConnection("custom://example.com", testChunk, nil)
	if err != nil {
		t.Fatalf("CreateConnection error: %v", err)
	}

	handler.UpdateConnection(conn, testChunk)

	headers := conn.GetHeaders()
	if headers["X-Updated"] != "true" {
		t.Error("Expected X-Updated header to be set after UpdateConnection")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	httpHandler, err := h.GetHandler(server.URL)
	if err != nil {
		t.Fatalf("Failed to get HTTP handler: %v", err)
	}

	conn, err = httpHandler.CreateConnection(server.URL, testChunk, nil)
	if err != nil {
		t.Fatalf("HTTP CreateConnection error: %v", err)
	}

	testChunk.Downloaded = 500
	httpHandler.UpdateConnection(conn, testChunk)

	headers = conn.GetHeaders()
	rangeHeader := headers["Range"]
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		t.Errorf("Expected Range header to start with 'bytes=', got %q", rangeHeader)
	}
}

func TestMultipleProtocolHandlers(t *testing.T) {
	h := protocol.NewHandler()

	proto1 := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
			"shared://example.com",
		},
	}

	proto2 := &CustomProtocol{
		canHandleURLs: []string{
			"unique://example.com",
			"shared://example.com",
		},
	}

	h.RegisterProtocol(proto1)
	h.RegisterProtocol(proto2)

	handler, err := h.GetHandler("shared://example.com")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if handler != proto1 {
		t.Error("Expected first registered protocol to handle shared URL")
	}

	handler, err = h.GetHandler("custom://example.com")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if handler != proto1 {
		t.Error("Expected first protocol to handle its unique URL")
	}

	handler, err = h.GetHandler("unique://example.com")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if handler != proto2 {
		t.Error("Expected second protocol to handle its unique URL")
	}
}

func TestConcurrentAccess(t *testing.T) {
	h := protocol.NewHandler()

	customProto := &CustomProtocol{
		canHandleURLs: []string{
			"custom://example.com",
		},
	}
	h.RegisterProtocol(customProto)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const goroutineCount = 10
	errChan := make(chan error, goroutineCount)

	for i := range goroutineCount {
		go func(index int) {
			var err error

			if index%2 == 0 {
				_, err = h.GetHandler(server.URL)
			} else {
				_, err = h.GetHandler("custom://example.com")
			}

			errChan <- err
		}(i)
	}

	for range goroutineCount {
		if err := <-errChan; err != nil {
			t.Errorf("Concurrent access error: %v", err)
		}
	}
}
