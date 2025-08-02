package peer

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type ManagerConfig struct {
	MaxPeers    int // hard limit (default 200)
	DialTimeout time.Duration
	ListenAddr  string
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
	listenAddr net.Addr     // actual listener address (nil if no listener)
	listener   net.Listener // store listener for proper shutdown

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
		m.listenAddr = l.Addr()
		m.listener = l
		m.wg.Add(1)
		go m.acceptLoop(l)
	}

	m.wg.Add(1)
	go m.eventLoop()

	return m
}

// acceptLoop handles incoming connections
func (m *Manager) acceptLoop(l net.Listener) {
	defer func() {
		m.wg.Done()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
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
	defer func() {
		m.wg.Done()
	}()

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
	m.evictIfNeeded()

	m.mu.RLock()
	connCount := len(m.conns)
	_, exists := m.conns[addr]
	m.mu.RUnlock()

	if exists {
		return
	}

	if connCount >= m.cfg.MaxPeers {
		return
	}

	conn, err := Dial(m.ctx, addr, m.cfg.InfoHash, m.cfg.PeerID)
	if err != nil {
		return
	}

	m.addConn(addr, conn)
}

// handleInbound processes an incoming connection
func (m *Manager) handleInbound(netConn net.Conn) {
	addr := netConn.RemoteAddr().String()

	m.evictIfNeeded()

	m.mu.RLock()
	connCount := len(m.conns)
	_, exists := m.conns[addr]
	m.mu.RUnlock()

	if exists {
		netConn.Close()
		return
	}

	if connCount >= m.cfg.MaxPeers {
		netConn.Close()
		return
	}

	err := m.serverHandshake(netConn)
	if err != nil {
		netConn.Close()
		return
	}

	pconn, err := m.createAcceptedConn(netConn)
	if err != nil {
		netConn.Close()
		return
	}

	m.addConn(addr, pconn)
}

// serverHandshake performs just the handshake part (read client, send response)
func (m *Manager) serverHandshake(netConn net.Conn) error {
	// Set read deadline for handshake
	if err := netConn.SetReadDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return err
	}

	buf := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(netConn, buf); err != nil {
		return fmt.Errorf("failed to read client handshake: %w", err)
	}

	clientHS, err := Unmarshal(buf)
	if err != nil {
		return fmt.Errorf("failed to unmarshal client handshake: %w", err)
	}

	if clientHS.InfoHash != m.cfg.InfoHash {
		return fmt.Errorf("info hash mismatch: expected %x, got %x", m.cfg.InfoHash, clientHS.InfoHash)
	}

	if err := netConn.SetWriteDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return err
	}

	serverHS := Handshake{
		Protocol: ProtocolID,
		InfoHash: m.cfg.InfoHash,
		PeerID:   m.cfg.PeerID,
	}

	responseBytes, err := serverHS.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal server handshake: %w", err)
	}

	if _, err := netConn.Write(responseBytes); err != nil {
		return fmt.Errorf("failed to send server handshake: %w", err)
	}

	netConn.SetReadDeadline(time.Time{})
	netConn.SetWriteDeadline(time.Time{})

	return nil
}

// createAcceptedConn creates a Conn object for an already-handshaken connection
func (m *Manager) createAcceptedConn(netConn net.Conn) (*Conn, error) {
	ctx, cancel := context.WithCancel(m.ctx)
	c := &Conn{
		netConn: netConn,
		r:       NewReader(netConn),
		w:       NewWriter(netConn),
		id:      m.cfg.PeerID,
		remote:  netConn.RemoteAddr(),
		ctx:     ctx,
		cancel:  cancel,
	}

	c.wg.Add(1)
	go func() {
		defer func() {
			c.wg.Done()
		}()

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
		conn.Close()
		return
	}

	m.conns[addr] = &ConnEntry{
		Conn:    conn,
		addedAt: time.Now(),
	}
}

// dropConn removes and closes a connection
func (m *Manager) dropConn(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, ok := m.conns[addr]; ok {
		entry.Conn.Close()
		delete(m.conns, addr)
	}
}

// closeAll closes all connections
func (m *Manager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range m.conns {
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

	// Close listener first to unblock Accept() calls
	if m.listener != nil {
		m.listener.Close()
	}

	// Cancel context to signal all goroutines to exit
	m.cancel()

	m.wg.Wait()
}
