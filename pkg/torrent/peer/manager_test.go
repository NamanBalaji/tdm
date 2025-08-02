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
		MaxPeers:    5,
		DialTimeout: time.Second,
		ListenAddr:  "",
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

	buf := make([]byte, peer.HandshakeLen)
	_, err := conn.Read(buf)
	if err != nil {
		return
	}

	hs, err := peer.Unmarshal(buf)
	if err != nil {
		return
	}

	if hs.InfoHash != s.infoHash {
		return
	}

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

	if count := m.Count(); count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

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

	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	addr := server.Addr().String()
	m.AddOutbound(addr)

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

	time.Sleep(100 * time.Millisecond)

	listenAddr := m.ListenAddr()
	if listenAddr == nil {
		t.Fatal("manager should have a listener address")
	}

	t.Logf("Dialing manager at: %s", listenAddr.String())

	conn, err := net.Dial("tcp", listenAddr.String())
	if err != nil {
		t.Fatalf("failed to dial manager at %s: %v", listenAddr.String(), err)
	}
	defer conn.Close()

	hs := peer.Handshake{
		Protocol: peer.ProtocolID,
		InfoHash: cfg.InfoHash,
		PeerID:   [20]byte{99, 98, 97, 96, 95, 94, 93, 92, 91, 90, 89, 88, 87, 86, 85, 84, 83, 82, 81, 80},
	}

	hsBytes, err := hs.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal handshake: %v", err)
	}

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write(hsBytes)
	if err != nil {
		t.Fatalf("failed to send handshake: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	response := make([]byte, peer.HandshakeLen)
	n, err := conn.Read(response)
	if err != nil {
		t.Fatalf("failed to read manager's handshake response: %v", err)
	}

	if n != peer.HandshakeLen {
		t.Fatalf("expected %d bytes, got %d", peer.HandshakeLen, n)
	}

	managerHS, err := peer.Unmarshal(response)
	if err != nil {
		t.Fatalf("failed to unmarshal manager's handshake: %v", err)
	}

	if managerHS.InfoHash != cfg.InfoHash {
		t.Fatalf("manager sent wrong info hash: expected %x, got %x", cfg.InfoHash, managerHS.InfoHash)
	}

	time.Sleep(200 * time.Millisecond)

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

	var servers []*mockPeerServer
	for i := 0; i < 5; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	for _, server := range servers {
		m.AddOutbound(server.Addr().String())
	}

	time.Sleep(500 * time.Millisecond)

	if count := m.Count(); count > cfg.MaxPeers {
		t.Errorf("expected count <= %d, got %d", cfg.MaxPeers, count)
	}
}

func TestManager_DuplicateConnections(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	addr := server.Addr().String()

	for i := 0; i < 3; i++ {
		m.AddOutbound(addr)
	}

	time.Sleep(300 * time.Millisecond)

	if count := m.Count(); count != 1 {
		t.Errorf("expected count 1 (no duplicates), got %d", count)
	}
}

func TestManager_RemovePeer(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer server.Stop()

	addr := server.Addr().String()
	m.AddOutbound(addr)

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
	m.RemovePeer(addr)

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

	time.Sleep(300 * time.Millisecond)

	initialCount := m.Count()
	if initialCount == 0 {
		t.Fatal("no connections established before shutdown test")
	}

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("shutdown timed out")
	}

	if count := m.Count(); count != 0 {
		t.Errorf("expected count 0 after shutdown, got %d", count)
	}
}

func TestManager_EvictionOrder(t *testing.T) {
	cfg := getTestConfig()
	cfg.MaxPeers = 2
	m := peer.NewManager(cfg)
	defer m.Stop()

	var servers []*mockPeerServer
	for i := 0; i < 4; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	m.AddOutbound(servers[0].Addr().String())
	time.Sleep(100 * time.Millisecond)

	m.AddOutbound(servers[1].Addr().String())
	time.Sleep(100 * time.Millisecond)

	if count := m.Count(); count != 2 {
		t.Errorf("expected 2 connections, got %d", count)
	}

	var addrs []string
	m.ForEach(func(addr string, c *peer.Conn) {
		addrs = append(addrs, addr)
	})

	m.AddOutbound(servers[2].Addr().String())
	time.Sleep(200 * time.Millisecond)

	if count := m.Count(); count != 2 {
		t.Errorf("expected 2 connections after eviction, got %d", count)
	}

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

	var servers []*mockPeerServer
	for i := 0; i < 15; i++ {
		server, err := newMockPeerServer(cfg.InfoHash, cfg.PeerID)
		if err != nil {
			t.Fatalf("failed to start mock server %d: %v", i, err)
		}
		defer server.Stop()
		servers = append(servers, server)
	}

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.AddOutbound(servers[idx].Addr().String())
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Count()
			m.ForEach(func(addr string, c *peer.Conn) {
			})
		}()
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	if count := m.Count(); count > cfg.MaxPeers {
		t.Errorf("expected count <= %d, got %d", cfg.MaxPeers, count)
	}
}

func TestManager_DialFailures(t *testing.T) {
	cfg := getTestConfig()
	m := peer.NewManager(cfg)
	defer m.Stop()

	invalidAddrs := []string{
		"127.0.0.1:1",
		"192.0.2.1:1234",
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
