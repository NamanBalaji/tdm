package connection_test

import (
	"errors"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/connection"
)

// dummyConn is a simple dummy implementation of the Connection interface.
type dummyConn struct {
	alive     bool
	closed    bool
	resetFail bool
}

func (d *dummyConn) Connect() error             { return nil }
func (d *dummyConn) Read(p []byte) (int, error) { return 0, nil }
func (d *dummyConn) Close() error {
	d.closed = true
	return nil
}
func (d *dummyConn) IsAlive() bool { return d.alive }
func (d *dummyConn) Reset() error {
	if d.resetFail {
		return errors.New("reset failed")
	}
	d.alive = true
	return nil
}
func (d *dummyConn) GetURL() string                   { return "http://dummy" }
func (d *dummyConn) GetHeaders() map[string]string    { return nil }
func (d *dummyConn) SetTimeout(timeout time.Duration) {}

func TestNewPoolAndStats(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	stats := pool.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected TotalConnections 0, got %d", stats.TotalConnections)
	}
	if stats.IdleConnections != 0 {
		t.Errorf("expected IdleConnections 0, got %d", stats.IdleConnections)
	}
	if stats.ActiveConnections != 0 {
		t.Errorf("expected ActiveConnections 0, got %d", stats.ActiveConnections)
	}
}

func TestRegisterConnection(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	conn := &dummyConn{alive: true}
	pool.RegisterConnection(conn)

	stats := pool.Stats()
	if stats.TotalConnections != 1 {
		t.Errorf("expected TotalConnections 1, got %d", stats.TotalConnections)
	}
	if stats.ActiveConnections != 1 {
		t.Errorf("expected ActiveConnections 1, got %d", stats.ActiveConnections)
	}
	if stats.IdleConnections != 0 {
		t.Errorf("expected IdleConnections 0, got %d", stats.IdleConnections)
	}
}

func TestGetConnectionAvailable(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	conn := &dummyConn{alive: true}
	pool.RegisterConnection(conn)
	// Release the connection so that it becomes available.
	pool.ReleaseConnection(conn)

	stats := pool.Stats()
	if stats.IdleConnections != 1 {
		t.Errorf("expected IdleConnections 1 after release, got %d", stats.IdleConnections)
	}

	retConn, err := pool.GetConnection("http://dummy", nil)
	if err != nil {
		t.Fatalf("GetConnection error: %v", err)
	}
	if retConn == nil {
		t.Fatal("expected a connection, got nil")
	}
	if retConn != conn {
		t.Error("returned connection does not match the registered connection")
	}

	stats = pool.Stats()
	if stats.ActiveConnections != 1 {
		t.Errorf("expected ActiveConnections 1 after GetConnection, got %d", stats.ActiveConnections)
	}
	if stats.IdleConnections != 0 {
		t.Errorf("expected IdleConnections 0 after GetConnection, got %d", stats.IdleConnections)
	}
}

func TestGetConnectionDead(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	// Create a connection that is dead and whose Reset fails.
	conn := &dummyConn{alive: false, resetFail: true}
	pool.RegisterConnection(conn)
	pool.ReleaseConnection(conn)

	retConn, err := pool.GetConnection("http://dummy", nil)
	if err != nil {
		t.Fatalf("GetConnection error: %v", err)
	}
	if retConn != nil {
		t.Error("expected nil connection when dead connection reset fails")
	}
	if !conn.closed {
		t.Error("expected dead connection to be closed")
	}
}

func TestReleaseConnection(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	conn := &dummyConn{alive: true}
	pool.RegisterConnection(conn)
	// Initially, the connection is in use.
	pool.ReleaseConnection(conn)

	stats := pool.Stats()
	if stats.IdleConnections != 1 {
		t.Errorf("expected IdleConnections 1 after release, got %d", stats.IdleConnections)
	}
	// Releasing the same connection again (which is no longer in use) should have no effect.
	pool.ReleaseConnection(conn)
	stats = pool.Stats()
	if stats.IdleConnections != 1 {
		t.Errorf("expected IdleConnections to remain 1 after duplicate release, got %d", stats.IdleConnections)
	}
}

func TestCloseAll(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)
	conn1 := &dummyConn{alive: true}
	conn2 := &dummyConn{alive: true}
	pool.RegisterConnection(conn1)
	pool.RegisterConnection(conn2)
	// Release one connection so that one becomes idle.
	pool.ReleaseConnection(conn1)

	pool.CloseAll()
	stats := pool.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected TotalConnections 0 after CloseAll, got %d", stats.TotalConnections)
	}
	if !conn1.closed {
		t.Error("expected conn1 to be closed after CloseAll")
	}
	if !conn2.closed {
		t.Error("expected conn2 to be closed after CloseAll")
	}
}
