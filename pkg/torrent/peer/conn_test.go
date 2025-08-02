package peer_test

import (
	"context"
	"crypto/rand"
	"github.com/NamanBalaji/tdm/pkg/torrent/peer"
	"net"
	"sync"
	"testing"
	"time"
)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func randomHash() [20]byte {
	var hash [20]byte
	rand.Read(hash[:])
	return hash
}

// Helper to create a test server that accepts connections
func createTestServer(t *testing.T, ctx context.Context, infoHash, peerID [20]byte) (net.Listener, chan *peer.Conn, chan error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	connChan := make(chan *peer.Conn, 10)
	errChan := make(chan error, 10)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := peer.Accept(ctx, listener, infoHash, peerID)
				if err != nil {
					select {
					case errChan <- err:
					case <-ctx.Done():
					}
					return
				}
				select {
				case connChan <- conn:
				case <-ctx.Done():
					conn.Close()
					return
				}
			}
		}
	}()

	return listener, connChan, errChan
}

func TestDialAndAccept(t *testing.T) {
	tests := []struct {
		name           string
		clientInfoHash [20]byte
		serverInfoHash [20]byte
		expectError    bool
		timeout        time.Duration
	}{
		{
			name:           "successful_handshake",
			clientInfoHash: randomHash(),
			serverInfoHash: func() [20]byte { h := randomHash(); return h }(), // Will be set to same as client
			expectError:    false,
			timeout:        5 * time.Second,
		},
		{
			name:           "info_hash_mismatch",
			clientInfoHash: randomHash(),
			serverInfoHash: randomHash(),
			expectError:    true,
			timeout:        5 * time.Second,
		},
		{
			name:           "timeout",
			clientInfoHash: randomHash(),
			serverInfoHash: randomHash(),
			expectError:    true,
			timeout:        100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "successful_handshake" {
				tt.serverInfoHash = tt.clientInfoHash // Same hash for success
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			serverPeerID := randomHash()
			clientPeerID := randomHash()

			listener, connChan, errChan := createTestServer(t, ctx, tt.serverInfoHash, serverPeerID)
			defer listener.Close()

			if tt.name == "timeout" {
				listener.Close()
			}

			var clientConn *peer.Conn
			var serverConn *peer.Conn
			var clientErr, serverErr error
			var wg sync.WaitGroup

			wg.Add(1)
			go func() {
				defer wg.Done()
				clientConn, clientErr = peer.Dial(ctx, listener.Addr().String(), tt.clientInfoHash, clientPeerID)
			}()

			if !tt.expectError {
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case serverConn = <-connChan:
					case serverErr = <-errChan:
					case <-ctx.Done():
						serverErr = ctx.Err()
					}
				}()
			}

			wg.Wait()

			// Check results
			if tt.expectError {
				if clientErr == nil {
					t.Error("expected client error but got none")
					if clientConn != nil {
						clientConn.Close()
					}
				}
			} else {
				if clientErr != nil {
					t.Errorf("unexpected client error: %v", clientErr)
				}
				if serverErr != nil {
					t.Errorf("unexpected server error: %v", serverErr)
				}
				if clientConn == nil {
					t.Error("client connection should not be nil")
				}
				if serverConn == nil {
					t.Error("server connection should not be nil")
				}

				if clientConn != nil {
					if clientConn.PeerID() != clientPeerID {
						t.Errorf("client peer ID mismatch: got %x, want %x", clientConn.PeerID(), clientPeerID)
					}
					if clientConn.RemoteAddr() == nil {
						t.Error("client remote addr should not be nil")
					}
				}

				if serverConn != nil {
					if serverConn.PeerID() != serverPeerID {
						t.Errorf("server peer ID mismatch: got %x, want %x", serverConn.PeerID(), serverPeerID)
					}
					if serverConn.RemoteAddr() == nil {
						t.Error("server remote addr should not be nil")
					}
				}
			}

			if clientConn != nil {
				clientConn.Close()
			}
			if serverConn != nil {
				serverConn.Close()
			}
		})
	}
}

func TestMessageExchange(t *testing.T) {
	type messageTest struct {
		name       string
		sendFunc   func(*peer.Conn) error
		expectType byte
		validate   func(t *testing.T, msg peer.Message)
	}

	tests := []messageTest{
		{
			name:       "choke",
			sendFunc:   (*peer.Conn).WriteChoke,
			expectType: peer.MsgChoke,
		},
		{
			name:       "unchoke",
			sendFunc:   (*peer.Conn).WriteUnchoke,
			expectType: peer.MsgUnchoke,
		},
		{
			name:       "interested",
			sendFunc:   (*peer.Conn).WriteInterested,
			expectType: peer.MsgInterested,
		},
		{
			name:       "not_interested",
			sendFunc:   (*peer.Conn).WriteNotInterested,
			expectType: peer.MsgNotInterested,
		},
		{
			name:       "keep_alive",
			sendFunc:   (*peer.Conn).WriteKeepAlive,
			expectType: peer.MsgKeepAlive,
		},
		{
			name: "have_message",
			sendFunc: func(c *peer.Conn) error {
				return c.WriteHave(42)
			},
			expectType: peer.MsgHave,
			validate: func(t *testing.T, msg peer.Message) {
				if msg.Index != 42 {
					t.Errorf("expected index 42, got %d", msg.Index)
				}
			},
		},
		{
			name: "request_message",
			sendFunc: func(c *peer.Conn) error {
				return c.WriteRequest(10, 1024, 16384)
			},
			expectType: peer.MsgRequest,
			validate: func(t *testing.T, msg peer.Message) {
				if msg.Index != 10 {
					t.Errorf("expected index 10, got %d", msg.Index)
				}
				if msg.Begin != 1024 {
					t.Errorf("expected begin 1024, got %d", msg.Begin)
				}
				if msg.Length != 16384 {
					t.Errorf("expected length 16384, got %d", msg.Length)
				}
			},
		},
		{
			name: "cancel_message",
			sendFunc: func(c *peer.Conn) error {
				return c.WriteCancel(5, 512, 8192)
			},
			expectType: peer.MsgCancel,
			validate: func(t *testing.T, msg peer.Message) {
				if msg.Index != 5 {
					t.Errorf("expected index 5, got %d", msg.Index)
				}
				if msg.Begin != 512 {
					t.Errorf("expected begin 512, got %d", msg.Begin)
				}
				if msg.Length != 8192 {
					t.Errorf("expected length 8192, got %d", msg.Length)
				}
			},
		},
		{
			name: "piece_message",
			sendFunc: func(c *peer.Conn) error {
				block := []byte{0xDE, 0xAD, 0xBE, 0xEF}
				return c.WritePiece(7, 256, block)
			},
			expectType: peer.MsgPiece,
			validate: func(t *testing.T, msg peer.Message) {
				if msg.Index != 7 {
					t.Errorf("expected index 7, got %d", msg.Index)
				}
				if msg.Begin != 256 {
					t.Errorf("expected begin 256, got %d", msg.Begin)
				}
				expected := []byte{0xDE, 0xAD, 0xBE, 0xEF}
				if len(msg.Block) != len(expected) {
					t.Errorf("expected block length %d, got %d", len(expected), len(msg.Block))
				}
				for i, b := range expected {
					if i < len(msg.Block) && msg.Block[i] != b {
						t.Errorf("block mismatch at %d: expected %02x, got %02x", i, b, msg.Block[i])
					}
				}
			},
		},
		{
			name: "bitfield_message",
			sendFunc: func(c *peer.Conn) error {
				bits := []byte{0xFF, 0x00, 0xAA, 0x55}
				return c.WriteBitfield(bits)
			},
			expectType: peer.MsgBitfield,
			validate: func(t *testing.T, msg peer.Message) {
				expected := []byte{0xFF, 0x00, 0xAA, 0x55}
				if len(msg.Payload) != len(expected) {
					t.Errorf("expected payload length %d, got %d", len(expected), len(msg.Payload))
				}
				for i, b := range expected {
					if i < len(msg.Payload) && msg.Payload[i] != b {
						t.Errorf("payload mismatch at %d: expected %02x, got %02x", i, b, msg.Payload[i])
					}
				}
			},
		},
		{
			name: "port_message",
			sendFunc: func(c *peer.Conn) error {
				return c.WritePort(6881)
			},
			expectType: peer.MsgPort,
			validate: func(t *testing.T, msg peer.Message) {
				if msg.Port != 6881 {
					t.Errorf("expected port 6881, got %d", msg.Port)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infoHash := randomHash()
			clientPeerID := randomHash()
			serverPeerID := randomHash()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			listener, connChan, errChan := createTestServer(t, ctx, infoHash, serverPeerID)
			defer listener.Close()

			clientConn, err := peer.Dial(ctx, listener.Addr().String(), infoHash, clientPeerID)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer clientConn.Close()

			var serverConn *peer.Conn
			select {
			case serverConn = <-connChan:
			case err := <-errChan:
				t.Fatalf("server error: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for server connection")
			}
			defer serverConn.Close()

			if err := tt.sendFunc(clientConn); err != nil {
				t.Fatalf("failed to send %s: %v", tt.name, err)
			}

			msg, err := serverConn.ReadMsg()
			if err != nil {
				t.Fatalf("failed to read %s: %v", tt.name, err)
			}

			if msg.Type != tt.expectType {
				t.Errorf("expected message type %d, got %d", tt.expectType, msg.Type)
			}

			if tt.validate != nil {
				tt.validate(t, msg)
			}
		})
	}
}

func TestConcurrentWrites(t *testing.T) {
	infoHash := randomHash()
	clientPeerID := randomHash()
	serverPeerID := randomHash()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, connChan, errChan := createTestServer(t, ctx, infoHash, serverPeerID)
	defer listener.Close()

	clientConn, err := peer.Dial(ctx, listener.Addr().String(), infoHash, clientPeerID)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	var serverConn *peer.Conn
	select {
	case serverConn = <-connChan:
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server connection")
	}
	defer serverConn.Close()

	const numGoroutines = 10
	const messagesPerGoroutine = 5
	totalMessages := numGoroutines * messagesPerGoroutine

	var wg sync.WaitGroup
	writeErrors := make(chan error, totalMessages)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				var err error
				switch (id + j) % 5 {
				case 0:
					err = clientConn.WriteChoke()
				case 1:
					err = clientConn.WriteUnchoke()
				case 2:
					err = clientConn.WriteInterested()
				case 3:
					err = clientConn.WriteNotInterested()
				case 4:
					err = clientConn.WriteKeepAlive()
				}
				if err != nil {
					writeErrors <- err
				}
			}
		}(i)
	}

	readDone := make(chan error, 1)
	go func() {
		for i := 0; i < totalMessages; i++ {
			_, err := serverConn.ReadMsg()
			if err != nil {
				readDone <- err
				return
			}
		}
		readDone <- nil
	}()

	wg.Wait()
	close(writeErrors)

	for err := range writeErrors {
		t.Errorf("write error: %v", err)
	}

	select {
	case err := <-readDone:
		if err != nil {
			t.Errorf("read error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for reads to complete")
	}
}

func TestReadTimeout(t *testing.T) {
	infoHash := randomHash()
	clientPeerID := randomHash()
	serverPeerID := randomHash()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, connChan, errChan := createTestServer(t, ctx, infoHash, serverPeerID)
	defer listener.Close()

	clientConn, err := peer.Dial(ctx, listener.Addr().String(), infoHash, clientPeerID)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	var serverConn *peer.Conn
	select {
	case serverConn = <-connChan:
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server connection")
	}
	defer serverConn.Close()

	start := time.Now()
	_, err = serverConn.ReadMsg()
	duration := time.Since(start)

	if err == nil {
		t.Error("expected timeout error but got none")
	}

	if duration < 1*time.Second {
		t.Errorf("timeout too fast: expected at least 1s, got %v", duration)
	}
	if duration > 3*time.Minute {
		t.Errorf("timeout too slow: expected around 2m, got %v", duration)
	}
}

func TestContextCancellation(t *testing.T) {
	infoHash := randomHash()
	peerID := randomHash()

	ctx, cancel := context.WithCancel(context.Background())

	listener, _, _ := createTestServer(t, ctx, infoHash, peerID)
	defer listener.Close()

	cancel()

	_, err := peer.Dial(ctx, listener.Addr().String(), infoHash, peerID)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestConnectionClosureAndCleanup(t *testing.T) {
	infoHash := randomHash()
	clientPeerID := randomHash()
	serverPeerID := randomHash()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, connChan, errChan := createTestServer(t, ctx, infoHash, serverPeerID)
	defer listener.Close()

	clientConn, err := peer.Dial(ctx, listener.Addr().String(), infoHash, clientPeerID)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	var serverConn *peer.Conn
	select {
	case serverConn = <-connChan:
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server connection")
	}

	start := time.Now()

	err1 := clientConn.Close()
	err2 := serverConn.Close()

	duration := time.Since(start)

	if duration > 1*time.Second {
		t.Errorf("close took too long: %v", duration)
	}

	_ = err1
	_ = err2

	err = clientConn.WriteChoke()
	if err == nil {
		t.Error("expected error when writing after close")
	}
}

func TestRawMessageAPI(t *testing.T) {
	infoHash := randomHash()
	clientPeerID := randomHash()
	serverPeerID := randomHash()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, connChan, errChan := createTestServer(t, ctx, infoHash, serverPeerID)
	defer listener.Close()

	clientConn, err := peer.Dial(ctx, listener.Addr().String(), infoHash, clientPeerID)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	var serverConn *peer.Conn
	select {
	case serverConn = <-connChan:
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server connection")
	}
	defer serverConn.Close()

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	err = clientConn.WriteMsg(peer.MsgHave, payload)
	if err != nil {
		t.Fatalf("WriteMsg failed: %v", err)
	}

	msg, err := serverConn.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg failed: %v", err)
	}

	if msg.Type != peer.MsgHave {
		t.Errorf("expected message type %d, got %d", peer.MsgHave, msg.Type)
	}

	if len(msg.Payload) != len(payload) {
		t.Errorf("payload length mismatch: expected %d, got %d", len(payload), len(msg.Payload))
	}

	for i, b := range payload {
		if i < len(msg.Payload) && msg.Payload[i] != b {
			t.Errorf("payload mismatch at %d: expected %02x, got %02x", i, b, msg.Payload[i])
		}
	}
}
