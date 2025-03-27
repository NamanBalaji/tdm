package connection

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// Pool is a struct that provides a reusable connection pool
type Pool struct {
	available    map[string][]Connection
	inUse        map[string][]Connection
	lastActivity map[uintptr]time.Time
	stats        PoolStats

	maxIdlePerHost int
	maxIdleTime    time.Duration

	mu sync.Mutex

	cleanupDone   chan struct{}
	cleanupCancel chan struct{}
}

// PoolStats contains statistics about the connection pool
type PoolStats struct {
	TotalConnections   int
	ActiveConnections  int
	IdleConnections    int
	ConnectionsCreated int64
	ConnectionsReused  int64
	MaxIdleConnections int
	ConnectTimeouts    int64
	ReadTimeouts       int64
}

// NewPool creates a new connection pool
func NewPool(maxIdlePerHost int, maxIdleTime time.Duration) *Pool {
	pool := &Pool{
		available:      make(map[string][]Connection),
		inUse:          make(map[string][]Connection),
		lastActivity:   make(map[uintptr]time.Time),
		maxIdlePerHost: maxIdlePerHost,
		maxIdleTime:    maxIdleTime,
		cleanupDone:    make(chan struct{}),
		cleanupCancel:  make(chan struct{}),
		stats: PoolStats{
			MaxIdleConnections: maxIdlePerHost,
		},
	}

	go pool.cleanup()

	return pool
}

// GetConnection retrieves a connection from the pool if available
// in case no suitable connection is found, it returns nil
// the caller is expected to create a new connection and register it with RegisterConnection
func (p *Pool) GetConnection(url string, headers map[string]string) (Connection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := hashConnection(url, headers)

	connections, ok := p.available[key]
	if !ok || len(connections) == 0 {
		// no available connections
		return nil, nil
	}

	lastIdx := len(connections) - 1
	conn := connections[lastIdx]

	p.available[key] = connections[:lastIdx]
	p.inUse[key] = append(p.inUse[key], conn)

	atomic.AddInt64(&p.stats.ConnectionsReused, 1)

	if !conn.IsAlive() {
		if err := conn.Reset(); err != nil {
			conn.Close()

			if idx := findConnectionIndex(p.inUse[key], conn); idx >= 0 {
				p.inUse[key] = append(p.inUse[key][:idx], p.inUse[key][idx+1:]...)
			}

			delete(p.lastActivity, getConnectionPtr(conn))
			return nil, fmt.Errorf("connection reset failed: %w", err)
		}
	}

	p.lastActivity[getConnectionPtr(conn)] = time.Now()

	return conn, nil
}

// RegisterConnection registers a newly created connection with the pool
// This must be called after creating a new connection not obtained from GetConnection
func (p *Pool) RegisterConnection(conn Connection) {
	if conn == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := hashConnection(conn.GetURL(), conn.GetHeaders())

	p.inUse[key] = append(p.inUse[key], conn)

	p.lastActivity[getConnectionPtr(conn)] = time.Now()

	atomic.AddInt64(&p.stats.ConnectionsCreated, 1)
	p.updateStats()
}

// ReleaseConnection returns a connection to the pool
func (p *Pool) ReleaseConnection(conn Connection) {
	if conn == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := hashConnection(conn.GetURL(), conn.GetHeaders())

	var found bool
	var idx int

	if connections, ok := p.inUse[key]; ok {
		for i, c := range connections {
			if c == conn {
				found = true
				idx = i
				break
			}
		}

		if found {
			p.inUse[key] = append(connections[:idx], connections[idx+1:]...)

			if conn.IsAlive() && len(p.available[key]) < p.maxIdlePerHost {
				p.available[key] = append(p.available[key], conn)
				p.lastActivity[getConnectionPtr(conn)] = time.Now()
			} else {
				conn.Close()
				delete(p.lastActivity, getConnectionPtr(conn))
			}
		}
	}

	p.updateStats()
}

// CloseAll closes all connections in the pool
func (p *Pool) CloseAll() {
	close(p.cleanupCancel)
	<-p.cleanupDone

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, connections := range p.available {
		for _, conn := range connections {
			conn.Close()
		}
	}

	for _, connections := range p.inUse {
		for _, conn := range connections {
			conn.Close()
		}
	}

	p.available = make(map[string][]Connection)
	p.inUse = make(map[string][]Connection)
	p.lastActivity = make(map[uintptr]time.Time)

	p.updateStats()
}

// Stats returns the current pool statistics
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.updateStats()
	return p.stats
}

// cleanup periodically removes idle connections
func (p *Pool) cleanup() {
	defer close(p.cleanupDone)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.removeIdleConnections()
		case <-p.cleanupCancel:
			return
		}
	}
}

// removeIdleConnections removes connections that have been idle for too long
func (p *Pool) removeIdleConnections() {
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	for key, connections := range p.available {
		var remaining []Connection

		for _, conn := range connections {
			connPtr := getConnectionPtr(conn)

			lastActive, exists := p.lastActivity[connPtr]
			if !exists {
				remaining = append(remaining, conn)
				p.lastActivity[connPtr] = now
				continue
			}

			if now.Sub(lastActive) > p.maxIdleTime {
				conn.Close()
				delete(p.lastActivity, connPtr)
			} else {
				remaining = append(remaining, conn)
			}
		}

		if len(remaining) > 0 {
			p.available[key] = remaining
		} else {
			delete(p.available, key)
		}
	}

	p.updateStats()
}

// updateStats updates the connection pool statistics
func (p *Pool) updateStats() {
	idle := 0
	for _, connections := range p.available {
		idle += len(connections)
	}

	active := 0
	for _, connections := range p.inUse {
		active += len(connections)
	}

	p.stats.IdleConnections = idle
	p.stats.ActiveConnections = active
	p.stats.TotalConnections = idle + active
}

// hashConnection creates a hash key for a connection based on URL and headers
func hashConnection(url string, headers map[string]string) string {
	key := url

	if headers != nil {
		if auth, ok := headers["Authorization"]; ok {
			key += "|auth:" + auth
		}
		if proxy, ok := headers["Proxy-Authorization"]; ok {
			key += "|proxy:" + proxy
		}
	}

	hash := md5.Sum([]byte(key))
	return hex.EncodeToString(hash[:])
}

// findConnectionIndex finds a connection's index in a slice
func findConnectionIndex(connections []Connection, target Connection) int {
	for i, conn := range connections {
		if conn == target {
			return i
		}
	}
	return -1
}

// getConnectionPtr returns a pointer to the connection as uintptr for use as map key
func getConnectionPtr(conn Connection) uintptr {
	return reflect.ValueOf(conn).Pointer()
}
