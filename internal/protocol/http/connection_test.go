package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpPackage "github.com/NamanBalaji/tdm/internal/protocol/http"
)

func TestNewConnection(t *testing.T) {
	url := "http://example.com/test.txt"
	headers := map[string]string{
		"Range":     "bytes=0-999",
		"UserAgent": "Test-Agent",
	}
	client := &http.Client{}
	startByte := int64(0)
	endByte := int64(999)

	conn := httpPackage.NewConnection(url, headers, client, startByte, endByte)
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
	conn := httpPackage.NewConnection("http://example.com", nil, &http.Client{}, 0, 100)

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
	conn := httpPackage.NewConnection("http://example.com", nil, &http.Client{}, 0, 100)

	timeout := 60 * time.Second
	conn.SetTimeout(timeout)
}

// Test connection with a real HTTP server.
func TestConnectionWithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	conn := httpPackage.NewConnection(
		server.URL,
		map[string]string{"Range": "bytes=0-999"},
		&http.Client{},
		0,
		999,
	)

	ctx := t.Context()
	err := conn.Connect(ctx)
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
	if !strings.Contains(data, "partial content") {
		t.Errorf("Expected to read 'partial content', got %q", data)
	}

	err = conn.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if conn.IsAlive() {
		t.Error("Connection should not be alive after Close")
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
	if !strings.Contains(data, "partial content") {
		t.Errorf("Expected to read 'partial content' after Reset, got %q", data)
	}
}

func TestConnectionWithDifferentStatusCodes(t *testing.T) {
	testCases := []struct {
		name        string
		statusCode  int
		expectError bool
	}{
		{"Success OK", http.StatusOK, false},
		{"Success Partial Content", http.StatusPartialContent, false},
		{"Error Not Found", http.StatusNotFound, true},
		{"Error Forbidden", http.StatusForbidden, true},
		{"Error Internal Server Error", http.StatusInternalServerError, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				if tc.statusCode < 400 {
					w.Write([]byte("test content"))
				} else {
					w.Write([]byte("error content"))
				}
			}))
			defer server.Close()

			conn := httpPackage.NewConnection(server.URL, nil, &http.Client{}, 0, 100)
			ctx := t.Context()

			err := conn.Connect(ctx)

			if tc.expectError && err == nil {
				t.Error("Expected error, got nil")
			} else if !tc.expectError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if !tc.expectError {
				if !conn.IsAlive() {
					t.Error("Connection should be alive after successful Connect")
				}

				// Read should work
				buf := make([]byte, 100)
				n, err := conn.Read(ctx, buf)
				if err != nil && err != io.EOF {
					t.Fatalf("Read failed: %v", err)
				}

				data := string(buf[:n])
				if !strings.Contains(data, "test content") {
					t.Errorf("Expected to read 'test content', got %q", data)
				}
			}
		})
	}
}

func TestReadWithoutConnecting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("auto-connected"))
	}))
	defer server.Close()

	conn := httpPackage.NewConnection(server.URL, nil, &http.Client{}, 0, 100)

	ctx := t.Context()
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL
	server.Close()

	conn := httpPackage.NewConnection(serverURL, nil, &http.Client{}, 0, 100)
	ctx := t.Context()

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

	conn := httpPackage.NewConnection(server.URL, nil, &http.Client{}, 0, 100)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := conn.Connect(ctx)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if conn.IsAlive() {
		t.Error("Connection should not be alive after timeout")
	}
}
