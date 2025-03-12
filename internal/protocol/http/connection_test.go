package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnect_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}

	if err := conn.Connect(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !conn.initialized {
		t.Fatal("expected connection to be initialized")
	}
}

func TestConnect_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 1 * time.Second,
	}

	err := conn.Connect()
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRead_Success(t *testing.T) {
	expected := "Hello, World!"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, expected)
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}

	if err := conn.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, len(expected))
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error: %v", err)
	}
	if n != len(expected) {
		t.Fatalf("expected %d bytes, got %d", len(expected), n)
	}
	if string(buf) != expected {
		t.Fatalf("expected %q, got %q", expected, string(buf))
	}
}

func TestRead_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}

	err := conn.Connect()
	if err == nil {
		t.Fatal("expected an error for server error status, got nil")
	}
}

func TestClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data")
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}

	if err := conn.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if conn.initialized {
		t.Fatal("expected connection not to be initialized after Close()")
	}
}

func TestIsAlive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}
	if conn.IsAlive() {
		t.Fatal("expected connection to be not alive before Connect()")
	}
	if err := conn.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if !conn.IsAlive() {
		t.Fatal("expected connection to be alive after Connect()")
	}
	conn.Close()
	if conn.IsAlive() {
		t.Fatal("expected connection to be not alive after Close()")
	}
}

func TestReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "reset test")
	}))
	defer server.Close()

	conn := &Connection{
		url:     server.URL,
		client:  &http.Client{},
		timeout: 5 * time.Second,
	}
	if err := conn.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if err := conn.Reset(); err != nil {
		t.Fatalf("Reset() error: %v", err)
	}
	if !conn.initialized {
		t.Fatal("expected connection to be initialized after Reset()")
	}
}

func TestSetTimeout(t *testing.T) {
	conn := &Connection{}
	timeout := 10 * time.Second
	conn.SetTimeout(timeout)
	if conn.timeout != timeout {
		t.Fatalf("expected timeout %v, got %v", timeout, conn.timeout)
	}
}

func TestGetURL(t *testing.T) {
	url := "http://example.com"
	conn := &Connection{
		url: url,
	}
	if got := conn.GetURL(); got != url {
		t.Fatalf("expected URL %q, got %q", url, got)
	}
}

func TestGetHeaders(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	conn := &Connection{
		headers: headers,
	}
	gotHeaders := conn.GetHeaders()
	if gotHeaders["Content-Type"] != headers["Content-Type"] {
		t.Fatalf("expected header %q, got %q", headers["Content-Type"], gotHeaders["Content-Type"])
	}
}
