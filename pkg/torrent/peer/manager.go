package peer

import (
	"context"
	"fmt"
	"github.com/NamanBalaji/tdm/internal/logger"
	"io"
	"net"
	"sync"
	"time"
)

type ManagerConfig struct {
	MaxPeers    int           // hard limit (default 200)
	DialTimeout time.Duration // inherited from T2.3
	ListenAddr  string        // ":6881" or empty for no inbound
	InfoHash    [20]byte
	PeerID      [20]byte
}

// ConnEntry wraps a connection with metadata for lifecycle management
type ConnEntry struct {
	Conn    *Conn
	addedAt time.Time
}

type Manager struct {
	cfg    ManagerConfig
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.RWMutex
	conns      map[string]*ConnEntry // key = net.Addr.String()
	wg         sync.WaitGroup
	listenAddr net.Addr // actual listener address (nil if no listener)

	// channels for async adds/removes
	dialCh   chan string   // "host:port"
	inCh     chan net.Conn // accepted conns
	removeCh chan string   // addr to drop
}

// NewManager creates a new peer connection manager
func NewManager(cfg ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(map[string]*ConnEntry),
		dialCh:   make(chan string, 128),
		inCh:     make(chan net.Conn, 32),
		removeCh: make(chan string, 32),
	}

	// start listener if requested
	if cfg.ListenAddr != "" {
		l, err := net.Listen("tcp", cfg.ListenAddr)
		if err != nil {
			panic(fmt.Sprintf("failed to listen on %s: %v", cfg.ListenAddr, err))
		}
		m.listenAddr = l.Addr() // store actual address
		m.wg.Add(1)
		go m.acceptLoop(l)
	}

	m.wg.Add(1)
	go m.eventLoop()

	return m
}

// acceptLoop handles incoming connections
func (m *Manager) acceptLoop(l net.Listener) {
	defer m.wg.Done()
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			// Listener closed, likely due to shutdown
			return
		}

		select {
		case m.inCh <- conn:
		case <-m.ctx.Done():
			conn.Close()
			return
		}
	}
}

// AddOutbound queues an outbound connection attempt
func (m *Manager) AddOutbound(addr string) {
	select {
	case m.dialCh <- addr:
	case <-m.ctx.Done():
	}
}

// RemovePeer queues a peer for removal (called when connection fails)
func (m *Manager) RemovePeer(addr string) {
	select {
	case m.removeCh <- addr:
	case <-m.ctx.Done():
	}
}

// eventLoop handles all connection lifecycle events in a single goroutine
func (m *Manager) eventLoop() {
	defer m.wg.Done()

	for {
		select {
		case addr := <-m.dialCh:
			m.dialPeer(addr)

		case conn := <-m.inCh:
			m.handleInbound(conn)

		case addr := <-m.removeCh:
			m.dropConn(addr)

		case <-m.ctx.Done():
			m.closeAll()
			return
		}
	}
}

// dialPeer attempts to connect to a peer
func (m *Manager) dialPeer(addr string) {
	// Check if we need to evict before adding
	m.evictIfNeeded()

	// Check again after potential eviction
	m.mu.RLock()
	connCount := len(m.conns)
	_, exists := m.conns[addr]
	m.mu.RUnlock()

	if exists {
		logger.Debugf("dial %s: already connected", addr)
		return
	}

	if connCount >= m.cfg.MaxPeers {
		logger.Debugf("dial %s: max peers reached (%d)", addr, m.cfg.MaxPeers)
		return
	}

	conn, err := Dial(m.ctx, addr, m.cfg.InfoHash, m.cfg.PeerID)
	if err != nil {
		logger.Debugf("dial %s failed: %v", addr, err)
		return
	}

	logger.Debugf("dial %s: connected successfully", addr)
	m.addConn(addr, conn)
}

// handleInbound processes an incoming connection
func (m *Manager) handleInbound(netConn net.Conn) {
	addr := netConn.RemoteAddr().String()
	logger.Debugf("handleInbound: processing connection from %s", addr)

	// Check if we need to evict before adding
	m.evictIfNeeded()

	// Check again after potential eviction
	m.mu.RLock()
	connCount := len(m.conns)
	_, exists := m.conns[addr]
	m.mu.RUnlock()

	if exists {
		logger.Debugf("inbound %s: duplicate connection", addr)
		netConn.Close()
		return
	}

	if connCount >= m.cfg.MaxPeers {
		logger.Debugf("inbound %s: max peers reached (%d)", addr, m.cfg.MaxPeers)
		netConn.Close()
		return
	}

	// Perform server-side handshake directly instead of using Accept
	logger.Debugf("handleInbound: performing server-side handshake for %s", addr)
	pconn, err := m.acceptHandshake(netConn)
	if err != nil {
		logger.Debugf("inbound %s: handshake failed: %v", addr, err)
		netConn.Close()
		return
	}

	logger.Debugf("inbound %s: connected successfully", addr)
	m.addConn(addr, pconn)
}

// acceptHandshake performs a server-side handshake (read client's handshake, then respond)
func (m *Manager) acceptHandshake(netConn net.Conn) (*Conn, error) {
	ctx, cancel := context.WithCancel(m.ctx)

	// Create the connection object first
	c := &Conn{
		netConn: netConn,
		r:       NewReader(netConn),
		w:       NewWriter(netConn),
		id:      m.cfg.PeerID,
		remote:  netConn.RemoteAddr(),
		ctx:     ctx,
		cancel:  cancel,
	}

	// Set read deadline for handshake
	if err := netConn.SetReadDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return nil, err
	}

	// Read client's handshake first (server behavior)
	buf := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(netConn, buf); err != nil {
		return nil, fmt.Errorf("failed to read client handshake: %w", err)
	}

	clientHS, err := Unmarshal(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal client handshake: %w", err)
	}

	// Verify info hash matches
	if clientHS.InfoHash != m.cfg.InfoHash {
		return nil, fmt.Errorf("info hash mismatch: expected %x, got %x", m.cfg.InfoHash, clientHS.InfoHash)
	}

	// Send our handshake response
	if err := netConn.SetWriteDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return nil, err
	}

	serverHS := Handshake{
		Protocol: ProtocolID,
		InfoHash: m.cfg.InfoHash,
		PeerID:   m.cfg.PeerID,
	}

	responseBytes, err := serverHS.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server handshake: %w", err)
	}

	if _, err := netConn.Write(responseBytes); err != nil {
		return nil, fmt.Errorf("failed to send server handshake: %w", err)
	}

	// Clear deadlines
	netConn.SetReadDeadline(time.Time{})
	netConn.SetWriteDeadline(time.Time{})

	// Start the connection monitor goroutine
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		<-c.ctx.Done()
		c.netConn.Close()
	}()

	return c, nil
}

// evictIfNeeded removes the oldest connection if at capacity
func (m *Manager) evictIfNeeded() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.conns) < m.cfg.MaxPeers {
		return
	}

	// Find oldest connection
	var oldestAddr string
	var oldestTime time.Time
	first := true

	for addr, entry := range m.conns {
		if first || entry.addedAt.Before(oldestTime) {
			oldestAddr = addr
			oldestTime = entry.addedAt
			first = false
		}
	}

	if oldestAddr != "" {
		logger.Debugf("evicting oldest connection: %s (added at %v)", oldestAddr, oldestTime)
		if entry, ok := m.conns[oldestAddr]; ok {
			entry.Conn.Close()
			delete(m.conns, oldestAddr)
		}
	}
}

// addConn adds a connection to the manager (assumes space is available)
func (m *Manager) addConn(addr string, conn *Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check for duplicates under lock
	if _, exists := m.conns[addr]; exists {
		logger.Debugf("addConn %s: duplicate detected, closing new connection", addr)
		conn.Close()
		return
	}

	m.conns[addr] = &ConnEntry{
		Conn:    conn,
		addedAt: time.Now(),
	}

	logger.Debugf("addConn %s: added successfully (total: %d)", addr, len(m.conns))
}

// dropConn removes and closes a connection
func (m *Manager) dropConn(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.conns[addr]; ok {
		logger.Debugf("dropConn %s: removing connection", addr)
		entry.Conn.Close()
		delete(m.conns, addr)
	}
}

// closeAll closes all connections
func (m *Manager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.Debugf("closeAll: closing %d connections", len(m.conns))
	for addr, entry := range m.conns {
		logger.Debugf("closeAll: closing %s", addr)
		entry.Conn.Close()
	}
	m.conns = make(map[string]*ConnEntry)
}

// ForEach calls f for each active connection; f must not block
func (m *Manager) ForEach(f func(addr string, c *Conn)) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for addr, entry := range m.conns {
		f(addr, entry.Conn)
	}
}

// Count returns current peer count
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

// ListenAddr returns the actual address the manager is listening on (nil if no listener)
func (m *Manager) ListenAddr() net.Addr {
	return m.listenAddr
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() {
	logger.Debugf("Stop: shutting down peer manager")
	m.cancel()
	m.wg.Wait()
	logger.Debugf("Stop: shutdown complete")
}
