package torrent

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// PeerState represents the state of a peer connection.
type PeerState struct {
	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool
}

// PeerConn represents a connection to a peer.
type PeerConn struct {
	conn         net.Conn
	peer         Peer
	infoHash     [20]byte
	peerID       [20]byte
	bitfield     *Bitfield
	state        PeerState
	reader       *bufio.Reader
	writer       *bufio.Writer
	mu           sync.RWMutex
	lastActivity time.Time
	extensions   [8]byte
}

// NewPeerConn creates a new peer connection.
func NewPeerConn(peer Peer, infoHash [20]byte, peerID [20]byte) (*PeerConn, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port), 30*time.Second)
	if err != nil {
		return nil, err
	}

	pc := &PeerConn{
		conn:         conn,
		peer:         peer,
		infoHash:     infoHash,
		peerID:       peerID,
		reader:       bufio.NewReader(conn),
		writer:       bufio.NewWriter(conn),
		state:        PeerState{AmChoking: true, PeerChoking: true},
		lastActivity: time.Now(),
	}

	return pc, nil
}

// Handshake performs the BitTorrent handshake.
func (pc *PeerConn) Handshake() error {
	// Send our handshake
	handshake := NewHandshake(pc.infoHash, pc.peerID)
	if err := handshake.Serialize(pc.writer); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}
	if err := pc.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush handshake: %w", err)
	}

	// Read peer's handshake
	pc.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	peerHandshake, err := ReadHandshake(pc.reader)
	if err != nil {
		return fmt.Errorf("failed to read handshake: %w", err)
	}
	pc.conn.SetReadDeadline(time.Time{})

	// Verify info hash
	if peerHandshake.InfoHash != pc.infoHash {
		return fmt.Errorf("info hash mismatch")
	}

	// Store extensions
	pc.extensions = peerHandshake.Reserved

	return nil
}

// SendMessage sends a message to the peer.
func (pc *PeerConn) SendMessage(msg *Message) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if err := msg.Serialize(pc.writer); err != nil {
		return err
	}
	if err := pc.writer.Flush(); err != nil {
		return err
	}

	pc.lastActivity = time.Now()
	return nil
}

// ReadMessage reads a message from the peer.
func (pc *PeerConn) ReadMessage(timeout time.Duration) (*Message, error) {
	if timeout > 0 {
		pc.conn.SetReadDeadline(time.Now().Add(timeout))
		defer pc.conn.SetReadDeadline(time.Time{})
	}

	msg, err := ReadMessage(pc.reader)
	if err != nil {
		return nil, err
	}

	pc.mu.Lock()
	pc.lastActivity = time.Now()
	pc.mu.Unlock()

	return msg, nil
}

// SendInterested sends an interested message.
func (pc *PeerConn) SendInterested() error {
	msg := &Message{Type: MsgInterested}
	if err := pc.SendMessage(msg); err != nil {
		return err
	}

	pc.mu.Lock()
	pc.state.AmInterested = true
	pc.mu.Unlock()

	return nil
}

// SendNotInterested sends a not interested message.
func (pc *PeerConn) SendNotInterested() error {
	msg := &Message{Type: MsgNotInterested}
	if err := pc.SendMessage(msg); err != nil {
		return err
	}

	pc.mu.Lock()
	pc.state.AmInterested = false
	pc.mu.Unlock()

	return nil
}

// SendChoke sends a choke message.
func (pc *PeerConn) SendChoke() error {
	msg := &Message{Type: MsgChoke}
	if err := pc.SendMessage(msg); err != nil {
		return err
	}

	pc.mu.Lock()
	pc.state.AmChoking = true
	pc.mu.Unlock()

	return nil
}

// SendUnchoke sends an unchoke message.
func (pc *PeerConn) SendUnchoke() error {
	msg := &Message{Type: MsgUnchoke}
	if err := pc.SendMessage(msg); err != nil {
		return err
	}

	pc.mu.Lock()
	pc.state.AmChoking = false
	pc.mu.Unlock()

	return nil
}

// SendRequest sends a block request.
func (pc *PeerConn) SendRequest(pieceIndex, offset, length int) error {
	req := &RequestMessage{
		Index:  uint32(pieceIndex),
		Begin:  uint32(offset),
		Length: uint32(length),
	}

	msg := &Message{
		Type:    MsgRequest,
		Payload: req.Serialize(),
	}

	return pc.SendMessage(msg)
}

// SendPiece sends a piece/block message.
func (pc *PeerConn) SendPiece(pieceIndex, offset int, data []byte) error {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(payload[0:4], uint32(pieceIndex))
	binary.BigEndian.PutUint32(payload[4:8], uint32(offset))
	copy(payload[8:], data)

	msg := &Message{
		Type:    MsgPiece,
		Payload: payload,
	}

	return pc.SendMessage(msg)
}

// SendHave sends a have message.
func (pc *PeerConn) SendHave(pieceIndex int) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(pieceIndex))

	msg := &Message{
		Type:    MsgHave,
		Payload: payload,
	}

	return pc.SendMessage(msg)
}

// SendBitfield sends our bitfield.
func (pc *PeerConn) SendBitfield(bitfield *Bitfield) error {
	msg := &Message{
		Type:    MsgBitfield,
		Payload: bitfield.Bytes(),
	}

	return pc.SendMessage(msg)
}

// SendKeepAlive sends a keep-alive message.
func (pc *PeerConn) SendKeepAlive() error {
	// Keep-alive is a message with length 0
	_, err := pc.conn.Write([]byte{0, 0, 0, 0})
	return err
}

// HandleMessage processes an incoming message.
func (pc *PeerConn) HandleMessage(msg *Message) error {
	switch msg.Type {
	case MsgChoke:
		pc.mu.Lock()
		pc.state.PeerChoking = true
		pc.mu.Unlock()

	case MsgUnchoke:
		pc.mu.Lock()
		pc.state.PeerChoking = false
		pc.mu.Unlock()

	case MsgInterested:
		pc.mu.Lock()
		pc.state.PeerInterested = true
		pc.mu.Unlock()

	case MsgNotInterested:
		pc.mu.Lock()
		pc.state.PeerInterested = false
		pc.mu.Unlock()

	case MsgHave:
		if len(msg.Payload) != 4 {
			return fmt.Errorf("invalid have message")
		}
		pieceIndex := int(binary.BigEndian.Uint32(msg.Payload))
		if pc.bitfield != nil {
			pc.bitfield.SetPiece(pieceIndex)
		}

	case MsgBitfield:
		// Note: bitfield should only be sent once after handshake
		if pc.bitfield == nil {
			// Initialize bitfield - caller needs to provide piece count
			return fmt.Errorf("bitfield not initialized")
		}
	}

	return nil
}

// IsChoked returns true if the peer is choking us.
func (pc *PeerConn) IsChoked() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.state.PeerChoking
}

// Close closes the peer connection.
func (pc *PeerConn) Close() error {
	return pc.conn.Close()
}
