package connection

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/NamanBalaji/tdm/internal/logger"
)

// PoolStats tracks statistics about the connection pool.
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

// ManagedConnection wraps a connection with metadata.
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

// Pool manages a pool of reusable connections.
type Pool struct {
	mu              sync.RWMutex
	hostConnections map[string][]*ManagedConnection

	// per-host slot semaphores to cap concurrent connections
	hostSlotsMu sync.Mutex
	hostSlots   map[string]*semaphore.Weighted

	maxPerHost  int64
	maxIdleTime time.Duration
	maxLifetime time.Duration

	stats         PoolStats
	cleanupTicker *time.Ticker
	done          chan struct{}
}

// NewPool creates a new connection pool.
func NewPool(maxPerHost int, maxIdleTime time.Duration) *Pool {
	if maxPerHost <= 0 {
		maxPerHost = 10
	}
	if maxIdleTime <= 0 {
		maxIdleTime = 5 * time.Minute
	}

	p := &Pool{
		hostConnections: make(map[string][]*ManagedConnection),
		hostSlots:       make(map[string]*semaphore.Weighted),
		maxPerHost:      int64(maxPerHost),
		maxIdleTime:     maxIdleTime,
		maxLifetime:     30 * time.Minute,
		done:            make(chan struct{}),
		stats:           PoolStats{MaxIdleConnections: maxPerHost},
	}
	p.cleanupTicker = time.NewTicker(2 * time.Minute)
	go p.periodicCleanup()

	logger.Infof("Connection pool created with maxPerHost=%d, maxIdleTime=%v", maxPerHost, maxIdleTime)
	return p
}

// hostSem returns or creates the per-host semaphore.
func (p *Pool) hostSem(host string) *semaphore.Weighted {
	p.hostSlotsMu.Lock()
	defer p.hostSlotsMu.Unlock()
	sem, ok := p.hostSlots[host]
	if !ok {
		sem = semaphore.NewWeighted(p.maxPerHost)
		p.hostSlots[host] = sem
	}
	return sem
}

// GetConnection blocks until a slot is available, then returns an idle conn or nil.
func (p *Pool) GetConnection(ctx context.Context, urlStr string, headers map[string]string) (Connection, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	host := u.Hostname()

	// 1) Acquire a host-slot (blocks fairly FIFO)
	if err := p.hostSem(host).Acquire(ctx, 1); err != nil {
		return nil, err
	}

	// 2) Cleanup expired connections and look for idle
	now := time.Now()
	p.mu.Lock()
	active := p.hostConnections[host][:0]
	for _, m := range p.hostConnections[host] {
		if !m.conn.IsAlive() || now.Sub(m.createdAt) > p.maxLifetime || (!m.inUse && now.Sub(m.lastUsedAt) > p.maxIdleTime) {
			m.conn.Close()
		} else {
			active = append(active, m)
		}
	}
	p.hostConnections[host] = active

	// reuse idle if headers compatible
	for _, m := range active {
		if !m.inUse && headersCompatible(m.headers, headers) {
			m.inUse = true
			m.lastUsedAt = now
			m.headers = copyHeaders(headers)
			atomic.AddInt64(&p.stats.ConnectionsReused, 1)
			p.updateStats()
			p.mu.Unlock()
			return m.conn, nil
		}
	}
	p.mu.Unlock()

	// no idle, caller must CreateConnection and then RegisterConnection
	return nil, nil
}

// RegisterConnection adds a new connection to the pool as in-use.
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

	m := &ManagedConnection{
		conn:       conn,
		url:        urlStr,
		host:       host,
		headers:    copyHeaders(conn.GetHeaders()),
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
		inUse:      true,
	}

	p.mu.Lock()
	p.hostConnections[host] = append(p.hostConnections[host], m)
	p.mu.Unlock()

	atomic.AddInt64(&p.stats.ConnectionsCreated, 1)
	p.updateStats()
}

// ReleaseConnection marks a connection idle and releases its host-slot.
func (p *Pool) ReleaseConnection(conn Connection) {
	if conn == nil {
		logger.Warnf("Attempted to release nil connection")
		return
	}
	urlStr := conn.GetURL()
	u, err := url.Parse(urlStr)
	if err == nil {
		host := u.Hostname()

		p.mu.Lock()
		for i, m := range p.hostConnections[host] {
			if m.conn == conn {
				if !conn.IsAlive() {
					conn.Close()
					p.hostConnections[host] = append(p.hostConnections[host][:i], p.hostConnections[host][i+1:]...)
				} else {
					m.inUse = false
					m.lastUsedAt = time.Now()
				}
				break
			}
		}
		p.mu.Unlock()

		// release host-slot
		p.hostSem(host).Release(1)
		p.updateStats()
	} else {
		// parsing error: still release a slot to avoid leak
		for _, sem := range p.hostSlots {
			sem.Release(1)
		}
	}
}

func (p *Pool) ReleaseSlot(urlStr string) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return
	}
	p.hostSem(u.Hostname()).Release(1)
}

// CleanupIdleConnections removes idle or expired connections from all hosts.
func (p *Pool) CleanupIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()
	logger.Debugf("Running idle connection cleanup")
	now := time.Now()
	for host, conns := range p.hostConnections {
		var active []*ManagedConnection
		for _, m := range conns {
			if m.inUse || (now.Sub(m.lastUsedAt) <= p.maxIdleTime && now.Sub(m.createdAt) <= p.maxLifetime) {
				active = append(active, m)
			} else {
				m.conn.Close()
			}
		}
		p.hostConnections[host] = active
	}
	p.updateStats()
}

// CloseAll closes all connections and stops cleanup.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
		close(p.done)
	}

	closed := 0
	for host, conns := range p.hostConnections {
		for _, m := range conns {
			m.conn.Close()
			closed++
		}
		delete(p.hostConnections, host)
	}
	logger.Infof("Closed %d connections in pool", closed)
	p.updateStats()
}

// Stats returns a snapshot of pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PoolStats{
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
}

// updateStats recalculates total/active/idle counts.
func (p *Pool) updateStats() {
	totalActive := int64(0)
	totalIdle := int64(0)

	for _, conns := range p.hostConnections {
		for _, m := range conns {
			if m.inUse {
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

// periodicCleanup runs CleanupIdleConnections every ticker interval.
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

// headersCompatible checks if two header maps are compatible for reuse.
func headersCompatible(stored, requested map[string]string) bool {
	authKeys := []string{"Authorization", "Proxy-Authorization", "Cookie"}
	for _, key := range authKeys {
		sv, sOK := stored[key]
		rv, rOK := requested[key]
		if sOK != rOK || (sOK && sv != rv) {
			return false
		}
	}
	return true
}

// copyHeaders returns a shallow copy of the headers map.
func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	hc := make(map[string]string, len(headers))
	for k, v := range headers {
		hc[k] = v
	}
	return hc
}
