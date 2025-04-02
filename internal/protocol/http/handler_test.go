package http_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/downloader"
	httpHandler "github.com/NamanBalaji/tdm/internal/protocol/http"
)

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
	tests := []struct {
		name   string
		urlStr string
		want   bool
	}{
		{"HTTP URL", "http://example.com", true},
		{"HTTPS URL", "https://example.com", true},
		{"FTP URL", "ftp://example.com", false},
		{"Invalid URL", "invalid-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.CanHandle(tt.urlStr); got != tt.want {
				t.Errorf("CanHandle(%q) = %v; want %v", tt.urlStr, got, tt.want)
			}
		})
	}
}

func TestInitializeWithHEAD(t *testing.T) {
	server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD method, got %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "1000")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(time.RFC1123))
		w.Header().Set("ETag", "\"test-etag\"")
		w.Header().Set("Content-Disposition", "attachment; filename=\"test.txt\"")
		w.WriteHeader(http.StatusOK)
	})

	h := httpHandler.NewHandler()
	ctx := context.Background()
	info, err := h.Initialize(ctx, server.URL, nil)

	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	if info.URL != server.URL {
		t.Errorf("expected URL %q, got %q", server.URL, info.URL)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("expected MimeType text/plain, got %q", info.MimeType)
	}
	if info.TotalSize != 1000 {
		t.Errorf("expected TotalSize 1000, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Error("expected SupportsRanges true")
	}
	if info.Filename != "test.txt" {
		t.Errorf("expected Filename test.txt, got %q", info.Filename)
	}
}

func TestInitializeWithRangeGET(t *testing.T) {
	server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	h := httpHandler.NewHandler()
	ctx := context.Background()
	info, err := h.Initialize(ctx, server.URL, nil)

	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	if info.TotalSize != 2000 {
		t.Errorf("expected TotalSize 2000, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Error("expected SupportsRanges true")
	}
}

func TestInitializeWithRegularGET(t *testing.T) {
	server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Full content"))
			return
		}

		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "3000")
			w.WriteHeader(http.StatusOK)
			if r.Header.Get("X-TDM-Get-Only-Headers") != "true" {
				w.Write([]byte("Full content"))
			}
			return
		}

		http.Error(w, "Unexpected request", http.StatusBadRequest)
	})

	h := httpHandler.NewHandler()
	ctx := context.Background()
	info, err := h.Initialize(ctx, server.URL, nil)

	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	if info.TotalSize != 3000 {
		t.Errorf("expected TotalSize 3000, got %d", info.TotalSize)
	}
	if info.SupportsRanges {
		t.Error("expected SupportsRanges false")
	}
	if info.MimeType != "application/pdf" {
		t.Errorf("expected MimeType application/pdf, got %q", info.MimeType)
	}
}

func TestInitializeWithServerError(t *testing.T) {
	server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	h := httpHandler.NewHandler()
	ctx := context.Background()
	_, err := h.Initialize(ctx, server.URL, nil)

	if err == nil {
		t.Fatal("expected error from Initialize, got nil")
	}

	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404, got: %v", err)
	}
}

func TestInitializeWithTimeout(t *testing.T) {
	server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	h := httpHandler.NewHandler()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := h.Initialize(ctx, server.URL, nil)

	if err == nil {
		t.Fatal("expected timeout error from Initialize, got nil")
	}

	if !strings.Contains(strings.ToLower(err.Error()), "context") &&
		!strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestFilenameExtraction(t *testing.T) {
	tests := []struct {
		name               string
		url                string
		contentDisposition string
		want               string
	}{
		{
			name:               "From Content-Disposition",
			url:                "http://example.com/path/to/something.html",
			contentDisposition: "attachment; filename=\"test-file.zip\"",
			want:               "test-file.zip",
		},
		{
			name:               "From URL",
			url:                "http://example.com/downloads/file.txt",
			contentDisposition: "",
			want:               "file.txt",
		},
		{
			name:               "Default",
			url:                "http://example.com/",
			contentDisposition: "",
			want:               "download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.contentDisposition != "" {
					w.Header().Set("Content-Disposition", tt.contentDisposition)
				}
				w.WriteHeader(http.StatusOK)
			})

			serverURL := server.URL
			if tt.url != "http://example.com/" {
				serverURL = server.URL + strings.TrimPrefix(tt.url, "http://example.com")
			}

			h := httpHandler.NewHandler()
			ctx := context.Background()
			info, err := h.Initialize(ctx, serverURL, nil)

			if err != nil {
				t.Fatalf("Initialize returned error: %v", err)
			}

			if info.Filename != tt.want {
				t.Errorf("expected filename %q, got %q", tt.want, info.Filename)
			}
		})
	}
}

func TestCreateConnection(t *testing.T) {
	h := httpHandler.NewHandler()
	ctx := context.Background()
	urlStr := "http://example.com/file.txt"

	tests := []struct {
		name       string
		chunk      *chunk.Chunk
		config     *downloader.Config
		wantHeader string
	}{
		{
			name: "New download",
			chunk: &chunk.Chunk{
				StartByte:  0,
				EndByte:    999,
				Downloaded: 0,
			},
			config:     nil,
			wantHeader: "bytes=0-999",
		},
		{
			name: "Resumed download",
			chunk: &chunk.Chunk{
				StartByte:  0,
				EndByte:    999,
				Downloaded: 200,
			},
			config:     nil,
			wantHeader: "bytes=200-999",
		},
		{
			name: "With custom headers",
			chunk: &chunk.Chunk{
				StartByte:  100,
				EndByte:    999,
				Downloaded: 50,
			},
			config: &downloader.Config{
				Headers: map[string]string{
					"Custom-Header": "test-value",
				},
			},
			wantHeader: "bytes=150-999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := h.CreateConnection(ctx, urlStr, tt.chunk, tt.config)
			if err != nil {
				t.Fatalf("CreateConnection returned error: %v", err)
			}

			headers := conn.GetHeaders()

			if range_header, exists := headers["Range"]; !exists || range_header != tt.wantHeader {
				t.Errorf("expected Range header %q, got %q", tt.wantHeader, range_header)
			}

			if ua, exists := headers["User-Agent"]; !exists || ua == "" {
				t.Error("expected User-Agent header to be set")
			}

			if tt.config != nil && tt.config.Headers != nil {
				for k, v := range tt.config.Headers {
					if headers[k] != v {
						t.Errorf("expected header %q to be %q, got %q", k, v, headers[k])
					}
				}
			}
		})
	}
}

type mockNetConn struct {
	reader *strings.Reader
}

func (m *mockNetConn) Read(b []byte) (n int, err error) {
	return m.reader.Read(b)
}

func (m *mockNetConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockNetConn) Close() error {
	return nil
}

func (m *mockNetConn) LocalAddr() net.Addr {
	return &mockAddr{}
}

func (m *mockNetConn) RemoteAddr() net.Addr {
	return &mockAddr{}
}

func (m *mockNetConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockNetConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockNetConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type mockAddr struct{}

func (a *mockAddr) Network() string {
	return "mock"
}

func (a *mockAddr) String() string {
	return "mock-addr"
}
