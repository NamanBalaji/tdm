package connection

import (
	"fmt"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/logger"
)

// PoolStats tracks statistics about the connection pool
type PoolStats struct {
	TotalConnections   int64
	ActiveConnections  int64
	IdleConnections    int64
	ConnectionsCreated int64
	ConnectionsReused  int64
	MaxIdleConnections int
	ConnectionTimeouts int64
	ConnectionFailures int64
	AverageConnectTime int64 // in milliseconds
}

// Pool manages a pool of reusable connections
type Pool struct {
	mu              sync.RWMutex
	hostConnections map[string][]*ManagedConnection
	maxPerHost      int
	maxIdleTime     time.Duration
	maxLifetime     time.Duration
	stats           PoolStats
	cleanupTicker   *time.Ticker
	done            chan struct{}
}

// ManagedConnection wraps a connection with metadata
type ManagedConnection struct {
	conn       Connection
	url        string
	host       string
	path       string
	headers    map[string]string
	createdAt  time.Time
	lastUsedAt time.Time
	totalBytes int64
	inUse      bool
}

// NewPool creates a new connection pool
func NewPool(maxPerHost int, maxIdleTime time.Duration) *Pool {
	if maxPerHost <= 0 {
		maxPerHost = 10
	}
	if maxIdleTime <= 0 {
		maxIdleTime = 5 * time.Minute
	}

	pool := &Pool{
		hostConnections: make(map[string][]*ManagedConnection),
		maxPerHost:      maxPerHost,
		maxIdleTime:     maxIdleTime,
		maxLifetime:     30 * time.Minute,
		done:            make(chan struct{}),
		stats: PoolStats{
			MaxIdleConnections: maxPerHost,
		},
	}

	// Start the cleanup goroutine
	pool.cleanupTicker = time.NewTicker(2 * time.Minute)
	go pool.periodicCleanup()

	logger.Infof("Connection pool created with maxPerHost=%d, maxIdleTime=%v", maxPerHost, maxIdleTime)
	return pool
}

// periodicCleanup removes idle connections periodically
func (p *Pool) periodicCleanup() {
	for {
		select {
		case <-p.cleanupTicker.C:
			p.CleanupIdleConnections()
		case <-p.done:
			return
		}
	}
}

// GetConnection gets a connection for the specified URL and headers
func (p *Pool) GetConnection(urlStr string, headers map[string]string) (Connection, bool, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Warnf("Failed to parse URL for connection: %v", err)
		return nil, false, fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()

	p.mu.Lock()
	defer p.mu.Unlock()

	connections, exists := p.hostConnections[host]
	if !exists || len(connections) == 0 {
		logger.Debugf("No connections found for host %s", host)
		return nil, false, nil
	}

	now := time.Now()
	logger.Debugf("Looking for available connection to %s among %d connections", host, len(connections))

	// First, clean up any dead connections to ensure an accurate count
	activeConnections := make([]*ManagedConnection, 0, len(connections))
	for _, managed := range connections {
		if !managed.conn.IsAlive() ||
			now.Sub(managed.createdAt) > p.maxLifetime ||
			(!managed.inUse && now.Sub(managed.lastUsedAt) > p.maxIdleTime) {

			logger.Debugf("Removing dead or expired connection to %s", host)
			managed.conn.Close()
			continue
		}
		activeConnections = append(activeConnections, managed)
	}

	// Update the connections list with only active connections
	p.hostConnections[host] = activeConnections
	connections = activeConnections

	// Now look for an available connection to use
	for _, managed := range connections {
		if !managed.inUse {
			if !headersCompatible(managed.headers, headers) {
				logger.Debugf("Headers don't match for connection to %s", host)
				continue
			}

			logger.Debugf("Reusing existing connection to %s", host)
			managed.inUse = true
			managed.lastUsedAt = now
			managed.headers = copyHeaders(headers)

			atomic.AddInt64(&p.stats.ConnectionsReused, 1)
			p.updateStats()
			return managed.conn, false, nil
		}
	}

	// Check if we're at the connection limit after cleaning up dead connections
	if len(connections) >= p.maxPerHost {
		logger.Debugf("Maximum connections (%d) reached for host %s", p.maxPerHost, host)
		return nil, true, nil
	}

	logger.Debugf("No suitable connection found for %s, can create new", host)
	p.updateStats()
	return nil, false, nil
}

// RegisterConnection adds a new connection to the pool
func (p *Pool) RegisterConnection(conn Connection) {
	if conn == nil {
		logger.Warnf("Attempted to register nil connection")
		return
	}

	urlStr := conn.GetURL()
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Warnf("Failed to parse URL for connection: %v", err)
		return
	}

	host := u.Hostname()
	path := u.Path

	managed := &ManagedConnection{
		conn:       conn,
		url:        urlStr,
		host:       host,
		path:       path,
		headers:    copyHeaders(conn.GetHeaders()),
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
		inUse:      true,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	logger.Debugf("Registering new connection to %s", host)

	if p.hostConnections == nil {
		p.hostConnections = make(map[string][]*ManagedConnection)
	}

	p.hostConnections[host] = append(p.hostConnections[host], managed)
	atomic.AddInt64(&p.stats.ConnectionsCreated, 1)
	p.updateStats()
}

// ReleaseConnection puts a connection back in the pool for reuse
func (p *Pool) ReleaseConnection(conn Connection) {
	if conn == nil {
		logger.Warnf("Attempted to release nil connection")
		return
	}

	logger.Debugf("Release called for connection")

	urlStr := conn.GetURL()
	u, err := url.Parse(urlStr)
	if err != nil {
		logger.Warnf("Failed to parse URL when releasing connection: %v", err)
		conn.Close()
		return
	}

	host := u.Hostname()

	p.mu.Lock()
	defer p.mu.Unlock()

	connections, exists := p.hostConnections[host]
	if !exists {
		logger.Warnf("No connections for host %s when releasing", host)
		conn.Close()
		return
	}

	logger.Debugf("Releasing connection to %s", host)

	for i, managed := range connections {
		if managed.conn == conn {
			if !conn.IsAlive() {
				logger.Debugf("Connection is dead, closing instead of releasing")
				conn.Close()
				p.removeConnection(host, i)
				p.updateStats()
				return
			}

			managed.inUse = false
			managed.lastUsedAt = time.Now()

			logger.Debugf("Connection marked as not in use and available for reuse")

			if len(connections) > p.maxPerHost {
				p.pruneConnectionsForHost(host)
			}

			p.updateStats()
			return
		}
	}

	logger.Warnf("Connection not found in pool when releasing, closing")
	conn.Close()
}

// pruneConnectionsForHost reduces the number of connections for a host to maxPerHost
func (p *Pool) pruneConnectionsForHost(host string) {
	connections := p.hostConnections[host]

	logger.Debugf("Too many connections for %s (%d/%d), pruning excess",
		host, len(connections), p.maxPerHost)

	sort.Slice(connections, func(i, j int) bool {
		if connections[i].inUse && !connections[j].inUse {
			return true
		}
		if !connections[i].inUse && connections[j].inUse {
			return false
		}
		return connections[i].lastUsedAt.After(connections[j].lastUsedAt)
	})

	excessCount := 0
	for i := len(connections) - 1; i >= 0 && len(connections)-excessCount > p.maxPerHost; i-- {
		if !connections[i].inUse {
			logger.Debugf("Closing excess connection to %s", host)
			connections[i].conn.Close()
			excessCount++

			connections[i] = connections[len(connections)-excessCount]
		}
	}

	if excessCount > 0 {
		p.hostConnections[host] = connections[:len(connections)-excessCount]
	}
}

// CleanupIdleConnections removes idle connections that haven't been used recently
func (p *Pool) CleanupIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	logger.Debugf("Running idle connection cleanup")
	now := time.Now()
	removed := 0

	for host, connections := range p.hostConnections {
		activeConnections := make([]*ManagedConnection, 0, len(connections))

		for _, managed := range connections {
			if managed.inUse {
				activeConnections = append(activeConnections, managed)
				continue
			}

			if now.Sub(managed.lastUsedAt) > p.maxIdleTime ||
				now.Sub(managed.createdAt) > p.maxLifetime {
				logger.Debugf("Cleaning up idle connection to %s (idle: %v, age: %v)",
					host, now.Sub(managed.lastUsedAt), now.Sub(managed.createdAt))
				managed.conn.Close()
				removed++
				continue
			}

			activeConnections = append(activeConnections, managed)
		}

		p.hostConnections[host] = activeConnections
	}

	logger.Debugf("Cleaned up %d idle connections", removed)
	p.updateStats()
}

// CloseAll closes all connections in the pool
func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	logger.Infof("Closing all connections in pool")

	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
		close(p.done)
	}

	closedCount := 0
	for host, connections := range p.hostConnections {
		for _, managed := range connections {
			managed.conn.Close()
			closedCount++
		}
		delete(p.hostConnections, host)
	}

	logger.Infof("Closed %d connections in pool", closedCount)
	p.updateStats()
}

// Stats returns statistics about the connection pool
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		TotalConnections:   atomic.LoadInt64(&p.stats.TotalConnections),
		ActiveConnections:  atomic.LoadInt64(&p.stats.ActiveConnections),
		IdleConnections:    atomic.LoadInt64(&p.stats.IdleConnections),
		ConnectionsCreated: atomic.LoadInt64(&p.stats.ConnectionsCreated),
		ConnectionsReused:  atomic.LoadInt64(&p.stats.ConnectionsReused),
		MaxIdleConnections: p.stats.MaxIdleConnections,
		ConnectionTimeouts: atomic.LoadInt64(&p.stats.ConnectionTimeouts),
		ConnectionFailures: atomic.LoadInt64(&p.stats.ConnectionFailures),
		AverageConnectTime: atomic.LoadInt64(&p.stats.AverageConnectTime),
	}

	return stats
}

// removeConnection removes a connection from the pool without closing it
func (p *Pool) removeConnection(host string, index int) {
	connections := p.hostConnections[host]
	if index < 0 || index >= len(connections) {
		return
	}

	lastIdx := len(connections) - 1
	connections[index] = connections[lastIdx]
	p.hostConnections[host] = connections[:lastIdx]

	if len(p.hostConnections[host]) == 0 {
		delete(p.hostConnections, host)
	}
}

// updateStats updates the pool statistics
func (p *Pool) updateStats() {
	totalActive := int64(0)
	totalIdle := int64(0)

	for _, connections := range p.hostConnections {
		for _, managed := range connections {
			if managed.inUse {
				totalActive++
			} else {
				totalIdle++
			}
		}
	}

	atomic.StoreInt64(&p.stats.ActiveConnections, totalActive)
	atomic.StoreInt64(&p.stats.IdleConnections, totalIdle)
	atomic.StoreInt64(&p.stats.TotalConnections, totalActive+totalIdle)
}

// headersCompatible checks if the supplied headers are compatible with the stored headers
func headersCompatible(stored, requested map[string]string) bool {
	// Check for authentication headers specifically
	authKeys := []string{
		"Authorization",
		"Proxy-Authorization",
		"Cookie",
	}

	for _, key := range authKeys {
		storedValue, storedExists := stored[key]
		requestedValue, requestedExists := requested[key]

		if storedExists && requestedExists && storedValue != requestedValue {
			return false
		}

		if storedExists != requestedExists {
			return false
		}
	}

	return true
}

// copyHeaders makes a copy of the headers map
func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	headerCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		headerCopy[k] = v
	}
	return headerCopy
}
