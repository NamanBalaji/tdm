package peer_test

import (
	"context"
	"github.com/NamanBalaji/tdm/pkg/torrent/peer"
	"net"
	"sync"
	"testing"
	"time"
)

const (
	testTimeout = 5 * time.Second
)

// Test helpers

func getTestConfig() peer.ManagerConfig {
	return peer.ManagerConfig{
		MaxPeers:    5, // Small number for testing
		DialTimeout: time.Second,
		ListenAddr:  "", // No listener by default
		InfoHash:    [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		PeerID:      [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
	}
}

func getTestConfigWithListener() peer.ManagerConfig {
	cfg := getTestConfig()
	cfg.ListenAddr = "127.0.0.1:0"
	return cfg
}

// Mock server that accepts connections and performs handshake
type mockPeerServer struct {
	listener net.Listener
	infoHash [20]byte
	peerID   [20]byte
	wg       sync.WaitGroup
	stopped  chan struct{}
}

func newMockPeerServer(infoHash, peerID [20]byte) (*mockPeerServer, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	s := &mockPeerServer{
		listener: l,
		infoHash: infoHash,
		peerID:   peerID,
		stopped:  make(chan struct{}),
	}

	s.wg.Add(1)
	go s.acceptLoop()

	return s, nil
}

func (s *mockPeerServer) acceptLoop() {
	defer s.wg.Done()
	defer close(s.stopped)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		go s.handleConnection(conn)
	}
}

func (s *mockPeerServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Simple handshake simulation - read handshake, send response
	buf := make([]byte, peer.HandshakeLen)
	_, err := conn.Read(buf)
	if err != nil {
		return
	}

	// Parse received handshake
	hs, err := peer.Unmarshal(buf)
	if err != nil {
		return
	}

	// Verify info hash matches
	if hs.InfoHash != s.infoHash {
		return
	}

	// Send response handshake
	responseHS := peer.Handshake{
		Protocol: peer.ProtocolID,
		InfoHash: s.infoHash,
		PeerID:   s.peerID,
	}

	response, err := responseHS.Marshal()
	if err != nil {
		return
	}

	conn.Write(response)

	// Keep connection alive briefly
	time.Sleep(100 * time.Millisecond)
}

func (s *mockPeerServer) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *mockPeerServer) Stop() {
	s.listener.Close()
	s.wg.Wait()
}

func TestManager_BasicOperations(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Test initial state
	if count := m.Count(); count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	// Test ForEach with empty manager
	callCount := 0
	m.ForEach(func(addr string, c *peer.Conn) {
		callCount++
	})
	if callCount != 0 {
		t.Errorf("expected 0 calls to ForEach, got %d", callCount)
	}
}

func TestManager_OutboundConnections(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start mock server
	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	// Add outbound connection
	addr := server.Addr().String()
	m.AddOutbound(addr)

	// Wait for connection to establish
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for connection")
		default:
			if m.Count() > 0 {
				goto connected
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

connected:
	if count := m.Count(); count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Test ForEach
	foundAddr := ""
	m.ForEach(func(addr string, c *peer.Conn) {
		foundAddr = addr
	})

	if foundAddr != addr {
		t.Errorf("expected addr %s, got %s", addr, foundAddr)
	}
}

func TestManager_InboundConnections(t *testing.T) {
	cfg := getTestConfigWithListener()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Give manager time to start listener
	time.Sleep(100 * time.Millisecond)

	// Get the actual listener address
	listenAddr := m.ListenAddr()
	if listenAddr == nil {
		t.Fatal("manager should have a listener address")
	}

	t.Logf("Dialing manager at: %s", listenAddr.String())

	// Dial the actual listener address
	conn, err := net.Dial("tcp", listenAddr.String())
	if err != nil {
		t.Fatalf("failed to dial manager at %s: %v", listenAddr.String(), err)
	}
	defer conn.Close()

	t.Logf("TCP connection established")

	// The issue is that both sides try to send first. In proper BitTorrent protocol:
	// - Client (us) should send handshake first
	// - Server (manager) should read first, then respond
	// But the current handshake function always sends first.

	// Let's work around this by having the test act like a client should:
	// Send handshake first
	hs := peer.Handshake{
		Protocol: peer.ProtocolID,
		InfoHash: cfg.InfoHash,
		PeerID:   [20]byte{99, 98, 97, 96, 95, 94, 93, 92, 91, 90, 89, 88, 87, 86, 85, 84, 83, 82, 81, 80},
	}

	hsBytes, err := hs.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal handshake: %v", err)
	}

	t.Logf("Sending handshake first (as client should)...")

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write(hsBytes)
	if err != nil {
		t.Fatalf("failed to send handshake: %v", err)
	}

	t.Logf("Handshake sent, now reading manager's response...")

	// Now read the manager's response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	response := make([]byte, peer.HandshakeLen)
	n, err := conn.Read(response)
	if err != nil {
		t.Fatalf("failed to read manager's handshake response: %v", err)
	}

	t.Logf("Read %d bytes from manager", n)

	if n != peer.HandshakeLen {
		t.Fatalf("expected %d bytes, got %d", peer.HandshakeLen, n)
	}

	// Verify the manager sent correct info hash
	managerHS, err := peer.Unmarshal(response)
	if err != nil {
		t.Fatalf("failed to unmarshal manager's handshake: %v", err)
	}

	if managerHS.InfoHash != cfg.InfoHash {
		t.Fatalf("manager sent wrong info hash: expected %x, got %x", cfg.InfoHash, managerHS.InfoHash)
	}

	t.Logf("Handshake completed successfully!")

	// Give manager time to process and add the connection
	time.Sleep(200 * time.Millisecond)

	// Check if connection was added
	if count := m.Count(); count != 1 {
		t.Errorf("expected count 1, got %d", count)
	} else {
		t.Logf("Connection successfully added to manager")
	}
}

func TestManager_MaxPeersEnforcement(t *testing.T) {
	cfg := getTestConfig()
	cfg.MaxPeers = 2 // Very small limit for testing
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start multiple mock servers
	var servers []*mockPeerServer
	for i := 0; i < 5; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	// Add connections beyond the limit
	for _, server := range servers {
		m.AddOutbound(server.Addr().String())
	}

	// Wait for connections to establish and settle
	time.Sleep(500 * time.Millisecond)

	// Should not exceed max peers
	if count := m.Count(); count > cfg.MaxPeers {
		t.Errorf("expected count <= %d, got %d", cfg.MaxPeers, count)
	}
}

func TestManager_DuplicateConnections(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start mock server
	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	addr := server.Addr().String()

	// Add same address multiple times
	for i := 0; i < 3; i++ {
		m.AddOutbound(addr)
	}

	// Wait for connections to establish
	time.Sleep(300 * time.Millisecond)

	// Should only have one connection
	if count := m.Count(); count != 1 {
		t.Errorf("expected count 1 (no duplicates), got %d", count)
	}
}

func TestManager_RemovePeer(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start mock server
	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	addr := server.Addr().String()
	m.AddOutbound(addr)

	// Wait for connection
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for connection")
		default:
			if m.Count() > 0 {
				goto connected
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

connected:
	// Remove the peer
	m.RemovePeer(addr)

	// Wait for removal
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for peer removal")
		default:
			if m.Count() == 0 {
				goto removed
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

removed:
	if count := m.Count(); count != 0 {
		t.Errorf("expected count 0 after removal, got %d", count)
	}
}

func TestManager_GracefulShutdown(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)

	// Start mock servers and connect
	var servers []*mockPeerServer
	for i := 0; i < 3; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
		m.AddOutbound(server.Addr().String())
	}

	// Wait for connections
	time.Sleep(300 * time.Millisecond)

	initialCount := m.Count()
	if initialCount == 0 {
		t.Fatal("no connections established before shutdown test")
	}

	// Stop manager
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	// Ensure shutdown completes within reasonable time
	select {
	case <-done:
		// Good
	case <-time.After(testTimeout):
		t.Fatal("shutdown timed out")
	}

	// Verify all connections are closed
	if count := m.Count(); count != 0 {
		t.Errorf("expected count 0 after shutdown, got %d", count)
	}
}

func TestManager_EvictionOrder(t *testing.T) {
	cfg := getTestConfig()
	cfg.MaxPeers = 2
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start mock servers
	var servers []*mockPeerServer
	for i := 0; i < 4; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	// Add first connection
	m.AddOutbound(servers[0].Addr().String())
	time.Sleep(100 * time.Millisecond)

	// Add second connection
	m.AddOutbound(servers[1].Addr().String())
	time.Sleep(100 * time.Millisecond)

	// Should have 2 connections now
	if count := m.Count(); count != 2 {
		t.Errorf("expected 2 connections, got %d", count)
	}

	// Record which addresses we have
	var addrs []string
	m.ForEach(func(addr string, c *peer.Conn) {
		addrs = append(addrs, addr)
	})

	// Add third connection - should evict oldest (first one)
	m.AddOutbound(servers[2].Addr().String())
	time.Sleep(200 * time.Millisecond)

	// Should still have 2 connections
	if count := m.Count(); count != 2 {
		t.Errorf("expected 2 connections after eviction, got %d", count)
	}

	// First connection should be gone
	foundFirst := false
	m.ForEach(func(addr string, c *peer.Conn) {
		if addr == servers[0].Addr().String() {
			foundFirst = true
		}
	})

	if foundFirst {
		t.Error("first connection should have been evicted")
	}
}

func TestManager_ConcurrentOperations(t *testing.T) {
	cfg := getTestConfig()
	cfg.MaxPeers = 10
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Start mock servers
	var servers []*mockPeerServer
	for i := 0; i < 15; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	// Concurrent adds
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.AddOutbound(servers[idx].Addr().String())
		}(i)
	}

	// Concurrent counts and iterations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Count()
			m.ForEach(func(addr string, c *peer.Conn) {
				// Just iterate
			})
		}()
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	// Should not exceed max peers
	if count := m.Count(); count > cfg.MaxPeers {
		t.Errorf("expected count <= %d, got %d", cfg.MaxPeers, count)
	}
}

func TestManager_DialFailures(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	// Try to connect to non-existent addresses
	invalidAddrs := []string{
		"127.0.0.1:1",    // Port 1 is usually not listening
		"192.0.2.1:1234", // RFC 5737 test address
		"invalid-host:1234",
	}

	for _, addr := range invalidAddrs {
		m.AddOutbound(addr)
	}

	time.Sleep(2 * time.Second)

	if count := m.Count(); count != 0 {
		t.Errorf("expected 0 connections from failed dials, got %d", count)
	}
}
