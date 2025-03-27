package http

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/downloader"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.client == nil {
		t.Fatal("expected non-nil http client in handler")
	}
}

func TestCanHandle(t *testing.T) {
	h := NewHandler()
	tests := []struct {
		urlStr string
		can    bool
	}{
		{"http://example.com", true},
		{"https://example.com", true},
		{"ftp://example.com", false},
		{"invalid-url", false},
	}
	for _, tt := range tests {
		if got := h.CanHandle(tt.urlStr); got != tt.can {
			t.Errorf("CanHandle(%q) = %v; want %v", tt.urlStr, got, tt.can)
		}
	}
}

func TestInitialize_HeadSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD method, got %s", r.Method)
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "123")
		now := time.Now().UTC().Truncate(time.Second)
		w.Header().Set("Last-Modified", now.Format(time.RFC1123))
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Disposition", `attachment; filename="test.txt"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	h := NewHandler()
	info, err := h.Initialize(ts.URL, &downloader.Config{})
	if err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if info.URL != ts.URL {
		t.Errorf("expected URL %q, got %q", ts.URL, info.URL)
	}
	if info.MimeType != "text/plain" {
		t.Errorf("expected MimeType text/plain, got %q", info.MimeType)
	}
	if info.TotalSize != 123 {
		t.Errorf("expected TotalSize 123, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Errorf("expected SupportsRanges true")
	}
	if info.Filename != "test.txt" {
		t.Errorf("expected Filename test.txt, got %q", info.Filename)
	}
}

func TestInitialize_RangeGET_Fallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet {
			expectedRange := "bytes=0-0"
			if r.Header.Get("Range") != expectedRange {
				t.Errorf("expected Range header %q, got %q", expectedRange, r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 0-0/500")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, "a") // one byte content
			return
		}
	}))
	defer ts.Close()

	h := NewHandler()
	info, err := h.Initialize(ts.URL, &downloader.Config{})
	if err != nil {
		t.Fatalf("Initialize (range GET) returned error: %v", err)
	}
	if info.TotalSize != 500 {
		t.Errorf("expected TotalSize 500, got %d", info.TotalSize)
	}
	if !info.SupportsRanges {
		t.Errorf("expected SupportsRanges true")
	}
}

func TestInitialize_RegularGET_Fallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("X-TDM-Get-Only-Headers") == "true" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "1000")
			now := time.Now().UTC().Truncate(time.Second)
			w.Header().Set("Last-Modified", now.Format(time.RFC1123))
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer ts.Close()

	h := NewHandler()
	info, err := h.Initialize(ts.URL, &downloader.Config{})
	if err != nil {
		t.Fatalf("Initialize (regular GET) returned error: %v", err)
	}
	if info.MimeType != "application/pdf" {
		t.Errorf("expected MimeType application/pdf, got %q", info.MimeType)
	}
	if info.TotalSize != 1000 {
		t.Errorf("expected TotalSize 1000, got %d", info.TotalSize)
	}
	if info.SupportsRanges {
		t.Errorf("expected SupportsRanges false, got true")
	}
}

func TestCreateConnection(t *testing.T) {
	h := NewHandler()
	urlStr := "http://example.com/file.txt"

	fullChunk := &chunk.Chunk{
		StartByte:  0,
		EndByte:    100,
		Downloaded: 0,
	}
	options := &downloader.Config{
		Headers: map[string]string{
			"Custom-Header": "custom",
		},
	}
	connInterface, err := h.CreateConnection(urlStr, fullChunk, options)
	if err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}
	conn, ok := connInterface.(*Connection)
	if !ok {
		t.Fatal("expected connection to be of type *Connection")
	}
	if ua := conn.headers["User-Agent"]; ua != defaultUserAgent {
		t.Errorf("expected User-Agent %q, got %q", defaultUserAgent, ua)
	}
	if val, exists := conn.headers["Custom-Header"]; !exists || val != "custom" {
		t.Errorf("expected Custom-Header to be set to 'custom'")
	}
	if _, exists := conn.headers["Range"]; exists {
		t.Errorf("did not expect Range header to be set for a full download")
	}

	// Test resumed download: for this scenario we simulate that the chunk represents a subset.
	// By having a non-zero StartByte, the condition in CreateConnection triggers the Range header.
	resumedChunk := &chunk.Chunk{
		StartByte:  10,  // non-zero start indicates partial download
		EndByte:    100, // end of chunk
		Downloaded: 5,   // already downloaded 5 bytes
	}
	connInterface2, err := h.CreateConnection(urlStr, resumedChunk, options)
	if err != nil {
		t.Fatalf("CreateConnection (resumed) returned error: %v", err)
	}
	conn2, ok := connInterface2.(*Connection)
	if !ok {
		t.Fatal("expected connection to be of type *Connection")
	}
	expectedRange := "bytes=15-100" // 10+5 to 100
	if rng, exists := conn2.headers["Range"]; !exists || rng != expectedRange {
		t.Errorf("expected Range header %q, got %q", expectedRange, rng)
	}
}

func TestParseContentDisposition(t *testing.T) {
	header := `attachment; filename="example.pdf"`
	filename := parseContentDisposition(header)
	if filename != "example.pdf" {
		t.Errorf("expected filename 'example.pdf', got %q", filename)
	}
	if res := parseContentDisposition(""); res != "" {
		t.Errorf("expected empty string, got %q", res)
	}
}

func TestExtractFilenameFromURL(t *testing.T) {
	urlStr := "http://example.com/path/to/file.zip"
	filename := extractFilenameFromURL(urlStr)
	if filename != "file.zip" {
		t.Errorf("expected 'file.zip', got %q", filename)
	}
	urlStr = "http://example.com/"
	filename = extractFilenameFromURL(urlStr)
	if filename != "download" {
		t.Errorf("expected 'download', got %q", filename)
	}
}

func TestParseLastModified(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	header := now.Format(time.RFC1123)
	parsed := parseLastModified(header)
	if parsed.IsZero() {
		t.Errorf("expected valid time, got zero")
	}
	parsed = parseLastModified("invalid time")
	if !parsed.IsZero() {
		t.Errorf("expected zero time for invalid header, got %v", parsed)
	}
}

// fakeConn is a stub for net.Conn to test headerOnlyConn.
type fakeConn struct {
	data   []byte
	closed bool
}

func (f *fakeConn) Read(b []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, io.EOF
	}
	n := copy(b, f.data)
	f.data = f.data[n:]
	return n, nil
}

func (f *fakeConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (f *fakeConn) Close() error {
	f.closed = true
	return nil
}

func (f *fakeConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (f *fakeConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// TestHeaderOnlyConn verifies that headerOnlyConn reads until header termination.
func TestHeaderOnlyConn(t *testing.T) {
	// Create a fake connection that returns header data ending with double CRLF.
	headerData := "HTTP/1.1 200 OK\r\nHeader: value\r\n\r\nBody starts here"
	fc := &fakeConn{data: []byte(headerData)}
	hoc := &headerOnlyConn{Conn: fc}

	buf := make([]byte, 1024)
	n, err := hoc.Read(buf)
	if err != io.EOF {
		t.Errorf("expected io.EOF after reading headers, got error: %v", err)
	}
	headerEndIndex := strings.Index(headerData, "\r\n\r\n")
	if headerEndIndex < 0 {
		t.Fatal("failed to find header termination in test data")
	}
	expected := headerData[:headerEndIndex+4]
	if string(buf[:n]) != expected {
		t.Errorf("expected header %q, got %q", expected, string(buf[:n]))
	}
}
