package http_test

import (
	"io"
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
	httpHandler "github.com/NamanBalaji/tdm/internal/protocol/http"
)

func createTestChunk(t *testing.T, startByte, endByte, downloaded int64) *chunk.Chunk {
	t.Helper()

	downloadID := uuid.New()
	c := chunk.NewChunk(downloadID, startByte, endByte, nil)

	if downloaded > 0 {
		c.Downloaded = downloaded
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

func setupTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
	})
	return server
}

func TestNewHandler(t *testing.T) {
	h := httpHandler.NewHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestCanHandle(t *testing.T) {
	h := httpHandler.NewHandler()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if !h.CanHandle(server.URL) {
		t.Errorf("Expected to handle %s", server.URL)
	}

	invalidURLs := []string{
		"invalid-url",
		"",
	}

	for _, url := range invalidURLs {
		if h.CanHandle(url) {
			t.Errorf("Expected not to handle invalid URL: %s", url)
		}
	}
}

func TestInitialize(t *testing.T) {
	h := httpHandler.NewHandler()
	ctx := t.Context()

	headServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(time.RFC1123))
			w.Header().Set("ETag", "\"test-etag\"")
			w.Header().Set("Content-Disposition", "attachment; filename=\"test.txt\"")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	info, err := h.Initialize(ctx, headServer.URL, nil)
	if err != nil {
		t.Fatalf("Initialize with HEAD request failed: %v", err)
	}

	if info.URL != headServer.URL {
		t.Errorf("Expected URL %q, got %q", headServer.URL, info.URL)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("Expected MimeType text/plain, got %q", info.MimeType)
	}
	if info.TotalSize != 1000 {
		t.Errorf("Expected TotalSize 1000, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Error("Expected SupportsRanges true")
	}
	if info.Filename != "test.txt" {
		t.Errorf("Expected Filename test.txt, got %q", info.Filename)
	}

	rangeServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
	})

	info, err = h.Initialize(ctx, rangeServer.URL, nil)
	if err != nil {
		t.Fatalf("Initialize with Range GET request failed: %v", err)
	}

	if info.TotalSize != 2000 {
		t.Errorf("Expected TotalSize 2000, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Error("Expected SupportsRanges true")
	}

	getServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
			// Don't support range requests
			w.WriteHeader(http.StatusOK)
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
	})

	info, err = h.Initialize(ctx, getServer.URL, nil)
	if err != nil {
		t.Fatalf("Initialize with regular GET request failed: %v", err)
	}

	if info.SupportsRanges {
		t.Error("Expected SupportsRanges to be false for regular GET")
	}

	errorServer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	_, err = h.Initialize(ctx, errorServer.URL, nil)
	if err == nil {
		t.Error("Expected error from Initialize with error server")
	}

	_, err = h.Initialize(ctx, "invalid://url", nil)
	if err == nil {
		t.Error("Expected error from Initialize with invalid URL")
	}
}

func TestCreateConnection(t *testing.T) {
	h := httpHandler.NewHandler()

	chunk1 := createTestChunk(t, 0, 999, 0)

	conn, err := h.CreateConnection("http://example.com/file.txt", chunk1, nil)
	if err != nil {
		t.Fatalf("CreateConnection failed: %v", err)
	}

	if conn == nil {
		t.Fatal("Expected non-nil connection")
	}

	headers := conn.GetHeaders()
	if rangeHeader, exists := headers["Range"]; !exists || rangeHeader != "bytes=0-999" {
		t.Errorf("Expected Range header 'bytes=0-999', got %q", rangeHeader)
	}

	// Test creating a connection for a partially downloaded chunk
	chunk2 := createTestChunk(t, 0, 999, 200)

	conn, err = h.CreateConnection("http://example.com/file.txt", chunk2, nil)
	if err != nil {
		t.Fatalf("CreateConnection failed: %v", err)
	}

	headers = conn.GetHeaders()
	if range_header, exists := headers["Range"]; !exists || range_header != "bytes=200-999" {
		t.Errorf("Expected Range header 'bytes=200-999', got %q", range_header)
	}

	// Test with custom config headers
	config := &common.Config{
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"User-Agent":    "Custom-UA/1.0",
		},
	}

	conn, err = h.CreateConnection("http://example.com/file.txt", chunk1, config)
	if err != nil {
		t.Fatalf("CreateConnection with custom headers failed: %v", err)
	}

	headers = conn.GetHeaders()
	if auth, exists := headers["Authorization"]; !exists || auth != "Bearer token123" {
		t.Errorf("Expected Authorization header 'Bearer token123', got %q", auth)
	}

	if ua, exists := headers["User-Agent"]; !exists || ua != "Custom-UA/1.0" {
		t.Errorf("Expected User-Agent header 'Custom-UA/1.0', got %q", ua)
	}
}

func TestUpdateConnection(t *testing.T) {
	h := httpHandler.NewHandler()

	chunk1 := createTestChunk(t, 0, 999, 0)
	conn, err := h.CreateConnection("http://example.com/file.txt", chunk1, nil)
	if err != nil {
		t.Fatalf("CreateConnection failed: %v", err)
	}

	chunk1.Downloaded = 500

	h.UpdateConnection(conn, chunk1)

	headers := conn.GetHeaders()
	if range_header, exists := headers["Range"]; !exists || range_header != "bytes=500-999" {
		t.Errorf("Expected Range header 'bytes=500-999', got %q", range_header)
	}
}

func TestHandlerWithServer(t *testing.T) {
	h := httpHandler.NewHandler()
	ctx := t.Context()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
			if len(parts) != 2 {
				http.Error(w, "Invalid range", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Range", "bytes "+parts[0]+"-"+parts[1]+"/1000")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("partial content"))
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("full content"))
	}))
	defer server.Close()

	info, err := h.Initialize(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !info.SupportsRanges {
		t.Error("Expected server to support ranges")
	}

	chunk1 := createTestChunk(t, 0, 999, 0)
	conn, err := h.CreateConnection(server.URL, chunk1, nil)
	if err != nil {
		t.Fatalf("CreateConnection failed: %v", err)
	}

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	buf := make([]byte, 100)
	n, err := conn.Read(ctx, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	data := string(buf[:n])
	if !strings.Contains(data, "content") {
		t.Errorf("Expected data to contain 'content', got %q", data)
	}

	chunk2 := createTestChunk(t, 0, 999, 200)
	conn2, err := h.CreateConnection(server.URL, chunk2, nil)
	if err != nil {
		t.Fatalf("CreateConnection failed: %v", err)
	}

	headers := conn2.GetHeaders()
	if rangeHeader, exists := headers["Range"]; !exists || rangeHeader != "bytes=200-999" {
		t.Errorf("Expected Range header 'bytes=200-999', got %q", rangeHeader)
	}
}
