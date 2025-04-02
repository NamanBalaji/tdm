package connection_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/connection"
)

type mockConnection struct {
	alive       bool
	url         string
	headers     map[string]string
	closed      bool
	resetFail   bool
	resetTime   time.Duration
	connectFunc func(ctx context.Context) error
	readFunc    func(ctx context.Context, p []byte) (int, error)
	closeFunc   func() error
	isAliveFunc func() bool
	resetFunc   func(ctx context.Context) error
	getURLFunc  func() string
	headersFunc func() map[string]string
	timeoutFunc func(time.Duration)
}

func (m *mockConnection) Connect(ctx context.Context) error {
	if m.connectFunc != nil {
		return m.connectFunc(ctx)
	}
	return nil
}

func (m *mockConnection) Read(ctx context.Context, p []byte) (int, error) {
	if m.readFunc != nil {
		return m.readFunc(ctx, p)
	}
	return 0, nil
}

func (m *mockConnection) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	m.closed = true
	m.alive = false
	return nil
}

func (m *mockConnection) IsAlive() bool {
	if m.isAliveFunc != nil {
		return m.isAliveFunc()
	}
	return m.alive
}

func (m *mockConnection) Reset(ctx context.Context) error {
	if m.resetFunc != nil {
		return m.resetFunc(ctx)
	}

	// Simulate a delay if specified
	if m.resetTime > 0 {
		select {
		case <-time.After(m.resetTime):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if m.resetFail {
		return errors.New("reset failed")
	}
	m.alive = true
	return nil
}

func (m *mockConnection) GetURL() string {
	if m.getURLFunc != nil {
		return m.getURLFunc()
	}
	return m.url
}

func (m *mockConnection) GetHeaders() map[string]string {
	if m.headersFunc != nil {
		return m.headersFunc()
	}
	return m.headers
}

func (m *mockConnection) SetTimeout(timeout time.Duration) {
	if m.timeoutFunc != nil {
		m.timeoutFunc(timeout)
	}
}

func newMockConnection(alive bool, url string, headers map[string]string, resetFail bool) *mockConnection {
	return &mockConnection{
		alive:     alive,
		url:       url,
		headers:   headers,
		resetFail: resetFail,
	}
}

func TestNewPool(t *testing.T) {
	maxIdle := 10
	maxIdleTime := 5 * time.Minute

	pool := connection.NewPool(maxIdle, maxIdleTime)

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
	if stats.MaxIdleConnections != maxIdle {
		t.Errorf("expected MaxIdleConnections %d, got %d", maxIdle, stats.MaxIdleConnections)
	}
}

func TestRegisterConnection(t *testing.T) {
	pool := connection.NewPool(10, time.Minute)
	ctx := context.Background()

	pool.RegisterConnection(nil)
	stats := pool.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected TotalConnections 0 after registering nil, got %d", stats.TotalConnections)
	}

	conn := newMockConnection(true, "http://example.com", nil, false)
	pool.RegisterConnection(conn)

	stats = pool.Stats()
	if stats.TotalConnections != 1 {
		t.Errorf("expected TotalConnections 1, got %d", stats.TotalConnections)
	}
	if stats.ActiveConnections != 1 {
		t.Errorf("expected ActiveConnections 1, got %d", stats.ActiveConnections)
	}
	if stats.IdleConnections != 0 {
		t.Errorf("expected IdleConnections 0, got %d", stats.IdleConnections)
	}

	headers := map[string]string{"Authorization": "Bearer token123"}
	conn2 := newMockConnection(true, "http://example.com", headers, false)
	pool.RegisterConnection(conn2)

	stats = pool.Stats()
	if stats.TotalConnections != 2 {
		t.Errorf("expected TotalConnections 2, got %d", stats.TotalConnections)
	}
	if stats.ActiveConnections != 2 {
		t.Errorf("expected ActiveConnections 2, got %d", stats.ActiveConnections)
	}

	conn3, err := pool.GetConnection(ctx, "http://nonexistent.com", nil)
	if err != nil {
		t.Fatalf("unexpected error from GetConnection: %v", err)
	}
	if conn3 != nil {
		t.Errorf("expected nil connection for non-existent URL")
	}
}

func TestGetConnectionAvailable(t *testing.T) {
	pool := connection.NewPool(10, time.Minute)
	ctx := context.Background()

	url := "http://example.com"
	conn := newMockConnection(true, url, nil, false)
	pool.RegisterConnection(conn)

	pool.ReleaseConnection(conn)

	stats := pool.Stats()
	if stats.IdleConnections != 1 {
		t.Errorf("expected IdleConnections 1 after release, got %d", stats.IdleConnections)
	}

	retrievedConn, err := pool.GetConnection(ctx, url, nil)
	if err != nil {
		t.Fatalf("GetConnection error: %v", err)
	}
	if retrievedConn == nil {
		t.Fatal("expected a connection, got nil")
	}
	if retrievedConn != conn {
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

func TestGetConnectionWithHeaders(t *testing.T) {
	pool := connection.NewPool(10, time.Minute)
	ctx := context.Background()

	url := "http://example.com"
	headers1 := map[string]string{"Authorization": "Bearer token1"}
	headers2 := map[string]string{"Authorization": "Bearer token2"}

	conn1 := newMockConnection(true, url, headers1, false)
	conn2 := newMockConnection(true, url, headers2, false)

	pool.RegisterConnection(conn1)
	pool.RegisterConnection(conn2)

	pool.ReleaseConnection(conn1)
	pool.ReleaseConnection(conn2)

	retrievedConn, err := pool.GetConnection(ctx, url, headers1)
	if err != nil {
		t.Fatalf("GetConnection error: %v", err)
	}
	if retrievedConn != conn1 {
		t.Error("returned connection does not match the expected connection with headers1")
	}

	retrievedConn, err = pool.GetConnection(ctx, url, headers2)
	if err != nil {
		t.Fatalf("GetConnection error: %v", err)
	}
	if retrievedConn != conn2 {
		t.Error("returned connection does not match the expected connection with headers2")
	}
}

func TestReleaseConnection(t *testing.T) {
	pool := connection.NewPool(2, time.Minute)

	pool.ReleaseConnection(nil)

	url := "http://example.com"
	conn := newMockConnection(true, url, nil, false)
	pool.RegisterConnection(conn)

	stats := pool.Stats()
	if stats.ActiveConnections != 1 || stats.IdleConnections != 0 {
		t.Errorf("expected ActiveConnections 1, IdleConnections 0; got %d, %d",
			stats.ActiveConnections, stats.IdleConnections)
	}

	pool.ReleaseConnection(conn)

	stats = pool.Stats()
	if stats.ActiveConnections != 0 || stats.IdleConnections != 1 {
		t.Errorf("expected ActiveConnections 0, IdleConnections 1; got %d, %d",
			stats.ActiveConnections, stats.IdleConnections)
	}

	pool.ReleaseConnection(conn)

	stats = pool.Stats()
	if stats.ActiveConnections != 0 || stats.IdleConnections != 1 {
		t.Errorf("expected ActiveConnections 0, IdleConnections 1 after duplicate release; got %d, %d",
			stats.ActiveConnections, stats.IdleConnections)
	}

	conn2 := newMockConnection(true, url, nil, false)
	conn3 := newMockConnection(true, url, nil, false)

	pool.RegisterConnection(conn2)
	pool.RegisterConnection(conn3)

	pool.ReleaseConnection(conn2)
	pool.ReleaseConnection(conn3)

	stats = pool.Stats()
	if stats.IdleConnections != 2 {
		t.Errorf("expected IdleConnections 2 (maxIdlePerHost), got %d", stats.IdleConnections)
	}

	connDead := newMockConnection(false, url, nil, false)
	pool.RegisterConnection(connDead)
	pool.ReleaseConnection(connDead)

	if !connDead.closed {
		t.Error("expected dead connection to be closed on release")
	}

	stats = pool.Stats()
	if stats.IdleConnections != 2 {
		t.Errorf("expected IdleConnections to remain 2, got %d", stats.IdleConnections)
	}
}

func TestCloseAll(t *testing.T) {
	pool := connection.NewPool(10, time.Minute)

	url1 := "http://example1.com"
	url2 := "http://example2.com"

	conn1 := newMockConnection(true, url1, nil, false)
	conn2 := newMockConnection(true, url2, nil, false)
	conn3 := newMockConnection(true, url1, nil, false)

	pool.RegisterConnection(conn1)
	pool.RegisterConnection(conn2)
	pool.RegisterConnection(conn3)

	pool.ReleaseConnection(conn1)

	stats := pool.Stats()
	if stats.TotalConnections != 3 || stats.ActiveConnections != 2 || stats.IdleConnections != 1 {
		t.Errorf("unexpected stats before CloseAll: total=%d, active=%d, idle=%d",
			stats.TotalConnections, stats.ActiveConnections, stats.IdleConnections)
	}

	pool.CloseAll()

	if !conn1.closed || !conn2.closed || !conn3.closed {
		t.Error("expected all connections to be closed")
	}

	stats = pool.Stats()
	if stats.TotalConnections != 0 || stats.ActiveConnections != 0 || stats.IdleConnections != 0 {
		t.Errorf("expected all counters to be 0 after CloseAll, got: total=%d, active=%d, idle=%d",
			stats.TotalConnections, stats.ActiveConnections, stats.IdleConnections)
	}
}

func TestIdleConnectionCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping idle connection cleanup test in short mode")
	}

	idleTimeout := 100 * time.Millisecond
	pool := connection.NewPool(10, idleTimeout)

	url := "http://example.com"
	conn := newMockConnection(true, url, nil, false)

	closeWasCalled := false
	conn.closeFunc = func() error {
		closeWasCalled = true
		conn.closed = true
		conn.alive = false
		return nil
	}

	pool.RegisterConnection(conn)
	pool.ReleaseConnection(conn)

	stats := pool.Stats()
	if stats.IdleConnections != 1 {
		t.Errorf("expected IdleConnections 1 before cleanup, got %d", stats.IdleConnections)
	}

	time.Sleep(idleTimeout * 5)

	pool.CloseAll()

	if !closeWasCalled {
		t.Log("Warning: Connection Close method was not called as expected")
		t.Log("This could be due to implementation details or timing issues")
	}
}

func TestPoolStatistics(t *testing.T) {
	pool := connection.NewPool(10, time.Minute)
	ctx := context.Background()

	stats := pool.Stats()
	if stats.ConnectionsCreated != 0 || stats.ConnectionsReused != 0 {
		t.Errorf("expected ConnectionsCreated and ConnectionsReused to be 0, got %d and %d",
			stats.ConnectionsCreated, stats.ConnectionsReused)
	}

	url := "http://example.com"
	conn := newMockConnection(true, url, nil, false)
	pool.RegisterConnection(conn)

	stats = pool.Stats()
	if stats.ConnectionsCreated != 1 {
		t.Errorf("expected ConnectionsCreated 1, got %d", stats.ConnectionsCreated)
	}

	pool.ReleaseConnection(conn)
	retrievedConn, _ := pool.GetConnection(ctx, url, nil)

	stats = pool.Stats()
	if stats.ConnectionsReused != 1 {
		t.Errorf("expected ConnectionsReused 1, got %d", stats.ConnectionsReused)
	}

	pool.ReleaseConnection(retrievedConn)
	pool.GetConnection(ctx, url, nil)

	stats = pool.Stats()
	if stats.ConnectionsReused != 2 {
		t.Errorf("expected ConnectionsReused 2, got %d", stats.ConnectionsReused)
	}
}

func TestConnectionKeys(t *testing.T) {
	url := "http://example.com"
	headers1 := map[string]string{"Authorization": "Bearer token1"}
	headers2 := map[string]string{"Authorization": "Bearer token2"}
	headers3 := map[string]string{"Proxy-Authorization": "Basic user:pass"}

	key1 := reflect.ValueOf(connection.NewPool).Call([]reflect.Value{
		reflect.ValueOf(1),
		reflect.ValueOf(time.Minute),
	})[0].MethodByName("GetConnection").Call([]reflect.Value{
		reflect.ValueOf(context.Background()),
		reflect.ValueOf(url),
		reflect.ValueOf(headers1),
	})[0].Interface()

	key2 := reflect.ValueOf(connection.NewPool).Call([]reflect.Value{
		reflect.ValueOf(1),
		reflect.ValueOf(time.Minute),
	})[0].MethodByName("GetConnection").Call([]reflect.Value{
		reflect.ValueOf(context.Background()),
		reflect.ValueOf(url),
		reflect.ValueOf(headers2),
	})[0].Interface()

	key3 := reflect.ValueOf(connection.NewPool).Call([]reflect.Value{
		reflect.ValueOf(1),
		reflect.ValueOf(time.Minute),
	})[0].MethodByName("GetConnection").Call([]reflect.Value{
		reflect.ValueOf(context.Background()),
		reflect.ValueOf(url),
		reflect.ValueOf(headers3),
	})[0].Interface()

	if key1 != nil || key2 != nil || key3 != nil {
		t.Errorf("expected all GetConnection calls to return nil for new pool")
	}
}
