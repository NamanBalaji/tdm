package connection_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/connection"
)

type dummyConnection struct {
	url     string
	headers map[string]string
	alive   bool
	closed  bool
}

func (d *dummyConnection) Connect(ctx context.Context) error               { return nil }
func (d *dummyConnection) Read(ctx context.Context, p []byte) (int, error) { return 0, nil }
func (d *dummyConnection) Close() error {
	d.closed = true
	d.alive = false
	return nil
}
func (d *dummyConnection) IsAlive() bool { return d.alive }
func (d *dummyConnection) Reset(ctx context.Context) error {
	if !d.alive {
		return errors.New("connection not alive")
	}
	return nil
}
func (d *dummyConnection) GetURL() string { return d.url }
func (d *dummyConnection) GetHeaders() map[string]string {
	copy := make(map[string]string)
	for k, v := range d.headers {
		copy[k] = v
	}
	return copy
}
func (d *dummyConnection) SetHeader(k, v string) {
	if d.headers == nil {
		d.headers = make(map[string]string)
	}
	d.headers[k] = v
}
func (d *dummyConnection) SetTimeout(timeout time.Duration) {}

func TestGetConnection_InvalidURL(t *testing.T) {
	p := connection.NewPool(1, time.Second)
	_, err := p.GetConnection(t.Context(), "%%%://bad-url", nil)
	if err == nil {
		t.Error("Expected error for invalid URL, got nil")
	}
}

func TestRegisterAndReuse(t *testing.T) {
	p := connection.NewPool(2, time.Second)
	url := "http://example.com"
	headers := map[string]string{"A": "1"}

	conn1, err := p.GetConnection(t.Context(), url, headers)
	if err != nil {
		t.Fatalf("Unexpected error on first GetConnection: %v", err)
	}
	if conn1 != nil {
		t.Fatalf("Expected nil conn on first GetConnection, got %#v", conn1)
	}

	dc := &dummyConnection{url: url, headers: headers, alive: true}
	p.RegisterConnection(dc)

	stats := p.Stats()
	if stats.ConnectionsCreated != 1 {
		t.Errorf("Expected ConnectionsCreated=1, got %d", stats.ConnectionsCreated)
	}
	if stats.TotalConnections != 1 {
		t.Errorf("Expected TotalConnections=1, got %d", stats.TotalConnections)
	}
	if stats.ActiveConnections != 1 {
		t.Errorf("Expected ActiveConnections=1, got %d", stats.ActiveConnections)
	}

	p.ReleaseConnection(dc)
	stats = p.Stats()
	if stats.ActiveConnections != 0 {
		t.Errorf("Expected ActiveConnections=0 after release, got %d", stats.ActiveConnections)
	}
	if stats.IdleConnections != 1 {
		t.Errorf("Expected IdleConnections=1 after release, got %d", stats.IdleConnections)
	}

	conn2, err := p.GetConnection(t.Context(), url, headers)
	if err != nil {
		t.Fatalf("Unexpected error on second GetConnection: %v", err)
	}
	if conn2 != dc {
		t.Fatalf("Expected to reuse same dummyConnection, got %#v", conn2)
	}
	stats = p.Stats()
	if stats.ConnectionsReused != 1 {
		t.Errorf("Expected ConnectionsReused=1, got %d", stats.ConnectionsReused)
	}
}

func TestMaxPerHostEnforcement(t *testing.T) {
	p := connection.NewPool(2, time.Second)
	url := "http://host.com"
	headers := map[string]string{}

	// Acquire two slots
	ctx := t.Context()
	_, err := p.GetConnection(ctx, url, headers)
	if err != nil {
		t.Fatalf("Failed to acquire first slot: %v", err)
	}
	_, err = p.GetConnection(ctx, url, headers)
	if err != nil {
		t.Fatalf("Failed to acquire second slot: %v", err)
	}

	timedCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = p.GetConnection(timedCtx, url, headers)
	if err == nil {
		t.Error("Expected timeout error on third GetConnection, got nil")
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Errorf("Expected GetConnection to block ~50ms, returned early: %v", time.Since(start))
	}
}

func TestReleaseSlot(t *testing.T) {
	p := connection.NewPool(1, time.Second)
	url := "http://release.com"
	headers := map[string]string{}

	_, err := p.GetConnection(t.Context(), url, headers)
	if err != nil {
		t.Fatalf("Failed to acquire slot: %v", err)
	}

	blocked := make(chan struct{})
	go func() {
		conn, err := p.GetConnection(t.Context(), url, headers)
		if err != nil {
			t.Errorf("Unexpected error after ReleaseSlot: %v", err)
		}
		if conn != nil {
			t.Errorf("Expected nil conn after ReleaseSlot, got %#v", conn)
		}
		close(blocked)
	}()

	time.Sleep(20 * time.Millisecond)

	p.ReleaseSlot(url)

	select {
	case <-blocked:
		// success
	case <-time.After(50 * time.Millisecond):
		t.Error("Blocked GetConnection did not resume after ReleaseSlot")
	}
}

func TestCleanupIdleConnections(t *testing.T) {
	p := connection.NewPool(1, 1*time.Millisecond)
	url := "http://clean.com"
	headers := map[string]string{}

	_, err := p.GetConnection(t.Context(), url, headers)
	if err != nil {
		t.Fatalf("Failed to acquire slot for cleanup test: %v", err)
	}

	dc := &dummyConnection{url: url, headers: headers, alive: true}
	p.RegisterConnection(dc)
	p.ReleaseConnection(dc)

	time.Sleep(2 * time.Millisecond)

	p.CleanupIdleConnections()
	stats := p.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("Expected 0 total connections after cleanup, got %d", stats.TotalConnections)
	}
}

func TestCloseAll(t *testing.T) {
	p := connection.NewPool(2, time.Second)
	url := "http://closeall.com"
	headers := map[string]string{}

	c1 := &dummyConnection{url: url, headers: headers, alive: true}
	c2 := &dummyConnection{url: url, headers: headers, alive: true}
	p.RegisterConnection(c1)
	p.RegisterConnection(c2)

	p.CloseAll()
	stats := p.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("Expected 0 total connections after CloseAll, got %d", stats.TotalConnections)
	}
}
