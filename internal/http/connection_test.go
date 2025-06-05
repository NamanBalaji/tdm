package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpConn "github.com/NamanBalaji/tdm/internal/http"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func TestNewConnection(t *testing.T) {
	url := "http://example.com/test.txt"
	headers := map[string]string{
		"Range":     "bytes=0-999",
		"UserAgent": "Test-Agent",
	}
	client := httpPkg.NewClient()
	startByte := int64(0)
	endByte := int64(999)

	conn := httpConn.NewConnection(url, headers, client, startByte, endByte)
	if conn == nil {
		t.Fatal("NewConnection returned nil")
	}

	if connURL := conn.GetURL(); connURL != url {
		t.Errorf("Expected URL %q, got %q", url, connURL)
	}

	connHeaders := conn.GetHeaders()
	if len(connHeaders) != len(headers) {
		t.Errorf("Expected %d headers, got %d", len(headers), len(connHeaders))
	}
	for k, v := range headers {
		if connHeaders[k] != v {
			t.Errorf("Expected header %q to be %q, got %q", k, v, connHeaders[k])
		}
	}

	headers["New-Header"] = "new-value"
	connHeaders = conn.GetHeaders()
	if _, exists := connHeaders["New-Header"]; exists {
		t.Error("Connection headers should be a copy, not a reference")
	}
}

func TestSetHeader(t *testing.T) {
	conn := httpConn.NewConnection("http://example.com", nil, httpPkg.NewClient(), 0, 100)

	conn.SetHeader("Test-Header", "test-value")
	headers := conn.GetHeaders()
	if headers["Test-Header"] != "test-value" {
		t.Errorf("Expected Test-Header to be test-value, got %q", headers["Test-Header"])
	}

	conn.SetHeader("Another-Header", "another-value")
	headers = conn.GetHeaders()
	if headers["Another-Header"] != "another-value" {
		t.Errorf("Expected Another-Header to be another-value, got %q", headers["Another-Header"])
	}

	conn.SetHeader("Test-Header", "new-value")
	headers = conn.GetHeaders()
	if headers["Test-Header"] != "new-value" {
		t.Errorf("Expected Test-Header to be new-value, got %q", headers["Test-Header"])
	}
}

func TestSetTimeout(t *testing.T) {
	conn := httpConn.NewConnection("http://example.com", nil, httpPkg.NewClient(), 0, 100)

	timeout := 60 * time.Second
	conn.SetTimeout(timeout)
}

func TestConnectionWithServer(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() *httptest.Server
		headers     map[string]string
		expectError bool
		checkData   string
	}{
		{
			name: "successful range request",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rangeHeader := r.Header.Get("Range")
					if rangeHeader != "" {
						w.Header().Set("Content-Type", "text/plain")
						w.WriteHeader(http.StatusPartialContent)
						w.Write([]byte("partial content"))
						return
					}
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("full content"))
				}))
			},
			headers:     map[string]string{"Range": "bytes=0-999"},
			expectError: false,
			checkData:   "partial content",
		},
		{
			name: "server doesn't support ranges",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("0123456789full content"))
				}))
			},
			headers:     map[string]string{"Range": "bytes=10-999"},
			expectError: false,
			checkData:   "full content",
		},
		{
			name: "server error",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			headers:     nil,
			expectError: true,
		},
		{
			name: "successful request without range",
			setup: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("full content"))
				}))
			},
			headers:     nil,
			expectError: false,
			checkData:   "full content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setup()
			defer server.Close()

			var startByte int64
			if tt.headers != nil && tt.headers["Range"] == "bytes=10-999" {
				startByte = 10
			}

			conn := httpConn.NewConnection(
				server.URL,
				tt.headers,
				httpPkg.NewClient(),
				startByte,
				999,
			)

			ctx := context.Background()
			err := conn.Connect(ctx)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}

			if !conn.IsAlive() {
				t.Error("Connection should be alive after successful Connect")
			}

			buf := make([]byte, 100)
			n, err := conn.Read(ctx, buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read failed: %v", err)
			}

			data := string(buf[:n])
			if !strings.Contains(data, tt.checkData) {
				t.Errorf("Expected to read %q, got %q", tt.checkData, data)
			}

			err = conn.Close()
			if err != nil {
				t.Errorf("Close failed: %v", err)
			}

			if conn.IsAlive() {
				t.Error("Connection should not be alive after Close")
			}
		})
	}
}

func TestConnectionLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test content"))
	}))
	defer server.Close()

	conn := httpConn.NewConnection(server.URL, nil, httpPkg.NewClient(), 0, 100)
	ctx := context.Background()

	// Test initial state
	if conn.IsAlive() {
		t.Error("Connection should not be alive initially")
	}

	// Test Connect
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !conn.IsAlive() {
		t.Error("Connection should be alive after Connect")
	}

	buf := make([]byte, 100)
	n, err := conn.Read(ctx, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	data := string(buf[:n])
	if !strings.Contains(data, "test content") {
		t.Errorf("Expected to read 'test content', got %q", data)
	}

	err = conn.Reset(ctx)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if !conn.IsAlive() {
		t.Error("Connection should be alive after Reset")
	}

	n, err = conn.Read(ctx, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read after Reset failed: %v", err)
	}

	data = string(buf[:n])
	if !strings.Contains(data, "test content") {
		t.Errorf("Expected to read 'test content' after Reset, got %q", data)
	}

	err = conn.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if conn.IsAlive() {
		t.Error("Connection should not be alive after Close")
	}
}

func TestReadWithoutConnecting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("auto-connected"))
	}))
	defer server.Close()

	conn := httpConn.NewConnection(server.URL, nil, httpPkg.NewClient(), 0, 100)

	ctx := context.Background()
	buf := make([]byte, 100)
	n, err := conn.Read(ctx, buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read (auto-connect) failed: %v", err)
	}

	data := string(buf[:n])
	if !strings.Contains(data, "auto-connected") {
		t.Errorf("Expected to read 'auto-connected', got %q", data)
	}

	if !conn.IsAlive() {
		t.Error("Connection should be alive after auto-connect")
	}
}

func TestConnectionNetworkErrors(t *testing.T) {
	conn := httpConn.NewConnection("http://localhost:0", nil, httpPkg.NewClient(), 0, 100)
	ctx := context.Background()

	err := conn.Connect(ctx)
	if err == nil {
		t.Error("Expected network error, got nil")
	}

	if conn.IsAlive() {
		t.Error("Connection should not be alive after failed Connect")
	}
}

func TestConnectionTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conn := httpConn.NewConnection(server.URL, nil, httpPkg.NewClient(), 0, 100)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := conn.Connect(ctx)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if conn.IsAlive() {
		t.Error("Connection should not be alive after timeout")
	}
}

func TestConnectionHeaders(t *testing.T) {
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	headers := map[string]string{
		"Authorization": "Bearer token123",
		"User-Agent":    "Custom-Agent/1.0",
		"Range":         "bytes=0-999",
	}

	conn := httpConn.NewConnection(server.URL, headers, httpPkg.NewClient(), 0, 999)
	ctx := context.Background()

	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	for key, value := range headers {
		if receivedHeaders.Get(key) != value {
			t.Errorf("Expected header %s=%s, got %s", key, value, receivedHeaders.Get(key))
		}
	}
}

func TestConnectionCloseWithoutOpen(t *testing.T) {
	conn := httpConn.NewConnection("http://example.com", nil, httpPkg.NewClient(), 0, 100)

	err := conn.Close()
	if err != nil {
		t.Errorf("Close on unopened connection should not error, got: %v", err)
	}
}

func TestConnectionReset(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))
	defer server.Close()

	conn := httpConn.NewConnection(server.URL, nil, httpPkg.NewClient(), 0, 100)
	ctx := context.Background()

	// First connection
	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 server call after Connect, got %d", callCount)
	}

	err = conn.Reset(ctx)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("Expected 2 server calls after Reset, got %d", callCount)
	}

	if !conn.IsAlive() {
		t.Error("Connection should be alive after Reset")
	}
}

func TestSetHeaderOnInitializedConnection(t *testing.T) {
	conn := httpConn.NewConnection("http://example.com", nil, httpPkg.NewClient(), 0, 100)

	conn.SetHeader("Initial", "value")

	headers := conn.GetHeaders()
	if headers["Initial"] != "value" {
		t.Errorf("Expected Initial header to be 'value', got %q", headers["Initial"])
	}

	conn.SetHeader("Additional", "extra")
	headers = conn.GetHeaders()
	if headers["Additional"] != "extra" {
		t.Errorf("Expected Additional header to be 'extra', got %q", headers["Additional"])
	}

	if len(headers) != 2 {
		t.Errorf("Expected 2 headers, got %d", len(headers))
	}
}
