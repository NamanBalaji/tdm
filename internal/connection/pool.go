package connection

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"
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
	logger.Debugf("Creating new connection pool: maxIdlePerHost=%d, maxIdleTime=%v",
		maxIdlePerHost, maxIdleTime)

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

	logger.Debugf("Starting connection pool cleanup goroutine")
	go pool.cleanup()

	logger.Debugf("Connection pool created successfully")
	return pool
}

// GetConnection retrieves a connection from the pool if available
// in case no suitable connection is found, it returns nil
// the caller is expected to create a new connection and register it with RegisterConnection
func (p *Pool) GetConnection(ctx context.Context, url string, headers map[string]string) (Connection, error) {
	key := hashConnection(url, headers)
	logger.Debugf("Getting connection for key: %s", key)

	p.mu.Lock()
	defer p.mu.Unlock()

	connections, ok := p.available[key]
	if !ok || len(connections) == 0 {
		logger.Debugf("No available connections for key %s", key)
		// no available connections
		return nil, nil
	}

	lastIdx := len(connections) - 1
	conn := connections[lastIdx]
	logger.Debugf("Found available connection for key %s", key)

	p.available[key] = connections[:lastIdx]
	p.inUse[key] = append(p.inUse[key], conn)

	atomic.AddInt64(&p.stats.ConnectionsReused, 1)
	logger.Debugf("Connection reuse count: %d", p.stats.ConnectionsReused)

	if !conn.IsAlive() {
		logger.Debugf("Connection not alive, attempting reset")
		if err := conn.Reset(ctx); err != nil {
			logger.Errorf("Failed to reset connection: %v", err)
			conn.Close()

			if idx := findConnectionIndex(p.inUse[key], conn); idx >= 0 {
				p.inUse[key] = append(p.inUse[key][:idx], p.inUse[key][idx+1:]...)
			}

			delete(p.lastActivity, getConnectionPtr(conn))
			return nil, fmt.Errorf("connection reset failed: %w", err)
		}
		logger.Debugf("Connection reset successful")
	}

	p.lastActivity[getConnectionPtr(conn)] = time.Now()
	logger.Debugf("Connection activity timestamp updated")

	return conn, nil
}

// RegisterConnection registers a newly created connection with the pool
// This must be called after creating a new connection not obtained from GetConnection
func (p *Pool) RegisterConnection(conn Connection) {
	if conn == nil {
		logger.Warnf("Attempted to register nil connection")
		return
	}

	url := conn.GetURL()
	headers := conn.GetHeaders()
	key := hashConnection(url, headers)

	logger.Debugf("Registering new connection for key: %s", key)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.inUse[key] = append(p.inUse[key], conn)
	p.lastActivity[getConnectionPtr(conn)] = time.Now()

	atomic.AddInt64(&p.stats.ConnectionsCreated, 1)
	logger.Debugf("Connection creation count: %d", p.stats.ConnectionsCreated)

	p.updateStats()
	logger.Debugf("Connection registered successfully")
}

// ReleaseConnection returns a connection to the pool
func (p *Pool) ReleaseConnection(conn Connection) {
	if conn == nil {
		logger.Warnf("Attempted to release nil connection")
		return
	}

	url := conn.GetURL()
	headers := conn.GetHeaders()
	key := hashConnection(url, headers)

	logger.Debugf("Releasing connection for key: %s", key)

	p.mu.Lock()
	defer p.mu.Unlock()

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
			logger.Debugf("Connection found in inUse pool, removing from active list")
			p.inUse[key] = append(connections[:idx], connections[idx+1:]...)

			if conn.IsAlive() && len(p.available[key]) < p.maxIdlePerHost {
				logger.Debugf("Connection is alive, adding to available pool")
				p.available[key] = append(p.available[key], conn)
				p.lastActivity[getConnectionPtr(conn)] = time.Now()
			} else {
				logger.Debugf("Connection is dead or pool is full, closing connection")
				conn.Close()
				delete(p.lastActivity, getConnectionPtr(conn))
			}
		} else {
			logger.Warnf("Connection not found in active list for key: %s", key)
		}
	} else {
		logger.Warnf("No active connections found for key: %s", key)
	}

	p.updateStats()
	logger.Debugf("Connection released successfully")
}

// CloseAll closes all connections in the pool
func (p *Pool) CloseAll() {
	logger.Infof("Closing all connections in pool")

	logger.Debugf("Stopping cleanup goroutine")
	close(p.cleanupCancel)
	<-p.cleanupDone
	logger.Debugf("Cleanup goroutine stopped")

	p.mu.Lock()
	defer p.mu.Unlock()

	// Close available connections
	availableCount := 0
	for key, connections := range p.available {
		for _, conn := range connections {
			logger.Debugf("Closing available connection for key: %s", key)
			conn.Close()
			availableCount++
		}
	}
	logger.Debugf("Closed %d available connections", availableCount)

	// Close in-use connections
	inUseCount := 0
	for key, connections := range p.inUse {
		for _, conn := range connections {
			logger.Debugf("Closing in-use connection for key: %s", key)
			conn.Close()
			inUseCount++
		}
	}
	logger.Debugf("Closed %d in-use connections", inUseCount)

	// Clear maps
	p.available = make(map[string][]Connection)
	p.inUse = make(map[string][]Connection)
	p.lastActivity = make(map[uintptr]time.Time)
	logger.Debugf("Cleared all connection maps")

	p.updateStats()
	logger.Infof("All connections closed successfully (%d total)", availableCount+inUseCount)
}

// Stats returns the current pool statistics
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.updateStats()
	logger.Debugf("Pool stats: total=%d, active=%d, idle=%d, created=%d, reused=%d",
		p.stats.TotalConnections, p.stats.ActiveConnections, p.stats.IdleConnections,
		p.stats.ConnectionsCreated, p.stats.ConnectionsReused)

	return p.stats
}

// cleanup periodically removes idle connections
func (p *Pool) cleanup() {
	logger.Debugf("Starting connection pool cleanup loop")
	defer close(p.cleanupDone)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Debugf("Running idle connection cleanup")
			p.removeIdleConnections()
		case <-p.cleanupCancel:
			logger.Debugf("Connection pool cleanup loop cancelled")
			return
		}
	}
}

// removeIdleConnections removes connections that have been idle for too long
func (p *Pool) removeIdleConnections() {
	now := time.Now()
	logger.Debugf("Removing idle connections (max idle time: %v)", p.maxIdleTime)

	p.mu.Lock()
	defer p.mu.Unlock()

	removedCount := 0
	checkedCount := 0

	for key, connections := range p.available {
		var remaining []Connection

		for _, conn := range connections {
			checkedCount++
			connPtr := getConnectionPtr(conn)

			lastActive, exists := p.lastActivity[connPtr]
			if !exists {
				logger.Debugf("Connection has no activity timestamp, keeping: %v", connPtr)
				remaining = append(remaining, conn)
				p.lastActivity[connPtr] = now
				continue
			}

			idleTime := now.Sub(lastActive)
			if idleTime > p.maxIdleTime {
				logger.Debugf("Closing idle connection: %v (idle for %v)", connPtr, idleTime)
				conn.Close()
				delete(p.lastActivity, connPtr)
				removedCount++
			} else {
				logger.Debugf("Keeping active connection: %v (idle for %v)", connPtr, idleTime)
				remaining = append(remaining, conn)
			}
		}

		if len(remaining) > 0 {
			p.available[key] = remaining
		} else {
			logger.Debugf("No remaining connections for key %s, removing key", key)
			delete(p.available, key)
		}
	}

	p.updateStats()
	logger.Debugf("Idle connection cleanup complete: checked=%d, removed=%d, remaining=%d",
		checkedCount, removedCount, p.stats.IdleConnections)
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
