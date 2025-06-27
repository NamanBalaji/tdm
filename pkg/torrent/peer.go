package torrent

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
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
	conn       net.Conn
	peer       Peer
	infoHash   [20]byte
	peerID     [20]byte
	bitfield   *Bitfield
	state      PeerState
	reader     *bufio.Reader
	writer     *bufio.Writer
	mu         sync.RWMutex
	extensions [8]byte

	// PEX and extension state
	supportsExtended bool
	utPexID          uint8 // Id for ut_pex message
	listeningPort    uint16

	// Rate-limiting and choking state
	downloadRate      int64 // Bytes per second from this peer
	uploadRate        int64 // Bytes per second to this peer
	lastBlockReceived time.Time

	// Counters for rate calculation over an interval
	downloadedSinceLastReview int64
	uploadedSinceLastReview   int64

	// Pending request tracking
	pendingRequests map[int]map[int]time.Time // pieceIndex -> blockOffset -> time
	pendingMu       sync.Mutex
}

// NewPeerConn creates a new peer connection.
func NewPeerConn(peer Peer, infoHash [20]byte, peerID [20]byte) (*PeerConn, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port), 30*time.Second)
	if err != nil {
		return nil, err
	}

	pc := &PeerConn{
		conn:            conn,
		peer:            peer,
		infoHash:        infoHash,
		peerID:          peerID,
		reader:          bufio.NewReader(conn),
		writer:          bufio.NewWriter(conn),
		state:           PeerState{AmChoking: true, PeerChoking: true},
		pendingRequests: make(map[int]map[int]time.Time),
	}

	return pc, nil
}

// Handshake performs the BitTorrent handshake.
func (pc *PeerConn) Handshake() (*Handshake, error) {
	handshake := NewHandshake(pc.infoHash, pc.peerID)
	err := handshake.Serialize(pc.writer)
	if err != nil {
		return nil, fmt.Errorf("failed to send handshake: %w", err)
	}
	err = pc.writer.Flush()

	if err != nil {
		return nil, fmt.Errorf("failed to flush handshake: %w", err)
	}

	pc.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	peerHandshake, err := ReadHandshake(pc.reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read handshake: %w", err)
	}

	pc.conn.SetReadDeadline(time.Time{})

	if peerHandshake.InfoHash != pc.infoHash {
		return nil, errors.New("info hash mismatch")
	}

	pc.extensions = peerHandshake.Reserved
	pc.supportsExtended = peerHandshake.HasExtensionSupport()

	return peerHandshake, nil
}

// SendMessage sends a message to the peer.
func (pc *PeerConn) SendMessage(msg *Message) error {
	// A length-prefix of 0 is a keep-alive message.
	if msg == nil {
		_, err := pc.conn.Write([]byte{0, 0, 0, 0})
		return err
	}

	buf := make([]byte, 4+1+len(msg.Payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(1+len(msg.Payload)))
	buf[4] = msg.Type
	copy(buf[5:], msg.Payload)

	_, err := pc.conn.Write(buf)

	return err
}

// SendExtendedHandshake sends our extended handshake to the peer.
func (pc *PeerConn) SendExtendedHandshake(port uint16) error {
	payload := map[string]any{
		"m": map[string]any{"ut_pex": 1},
		"p": port,
		"v": "TDM v0.1",
	}

	bencodedPayload, err := bencode.Encode(payload)
	if err != nil {
		return err
	}

	extendedMsgPayload := make([]byte, 1+len(bencodedPayload))
	extendedMsgPayload[0] = ExtendedHandshakeID
	copy(extendedMsgPayload[1:], bencodedPayload)

	msg := &Message{
		Type:    MsgExtended,
		Payload: extendedMsgPayload,
	}

	return pc.SendMessage(msg)
}

// SendPEX sends a peer exchange message.
func (pc *PeerConn) SendPEX(added, dropped []Peer) error {
	pc.mu.RLock()
	pexID := pc.utPexID
	pc.mu.RUnlock()

	if pexID == 0 {
		return errors.New("peer does not support ut_pex")
	}

	var addedBytes, droppedBytes []byte

	for _, p := range added {
		ip := p.IP.To4()
		if ip != nil {
			addedBytes = append(addedBytes, ip...)
			addedBytes = binary.BigEndian.AppendUint16(addedBytes, p.Port)
		}
	}

	for _, p := range dropped {
		ip := p.IP.To4()
		if ip != nil {
			droppedBytes = append(droppedBytes, ip...)
			droppedBytes = binary.BigEndian.AppendUint16(droppedBytes, p.Port)
		}
	}

	pexPayload := map[string]any{"added": addedBytes, "dropped": droppedBytes}

	bencodedPayload, err := bencode.Encode(pexPayload)
	if err != nil {
		return err
	}

	extendedMsgPayload := make([]byte, 1+len(bencodedPayload))
	extendedMsgPayload[0] = pexID
	copy(extendedMsgPayload[1:], bencodedPayload)

	msg := &Message{
		Type:    MsgExtended,
		Payload: extendedMsgPayload,
	}

	return pc.SendMessage(msg)
}

// ReadMessage reads a message from the peer.
func (pc *PeerConn) ReadMessage(timeout time.Duration) (*Message, error) {
	if timeout > 0 {
		pc.conn.SetReadDeadline(time.Now().Add(timeout))
		defer pc.conn.SetReadDeadline(time.Time{})
	}

	return ReadMessage(pc.reader)
}

// SendInterested sends an interested message.
func (pc *PeerConn) SendInterested() error {
	pc.mu.Lock()

	if pc.state.AmInterested {
		pc.mu.Unlock()
		return nil
	}

	pc.state.AmInterested = true
	pc.mu.Unlock()

	msg := &Message{Type: MsgInterested}

	return pc.SendMessage(msg)
}

// SendNotInterested sends a not interested message.
func (pc *PeerConn) SendNotInterested() error {
	pc.mu.Lock()

	if !pc.state.AmInterested {
		pc.mu.Unlock()
		return nil
	}

	pc.state.AmInterested = false
	pc.mu.Unlock()

	msg := &Message{Type: MsgNotInterested}

	return pc.SendMessage(msg)
}

// SendChoke sends a choke message.
func (pc *PeerConn) SendChoke() error {
	pc.mu.Lock()

	if pc.state.AmChoking {
		pc.mu.Unlock()
		return nil
	}

	pc.state.AmChoking = true
	pc.mu.Unlock()

	msg := &Message{Type: MsgChoke}

	return pc.SendMessage(msg)
}

// SendUnchoke sends an unchoke message.
func (pc *PeerConn) SendUnchoke() error {
	pc.mu.Lock()

	if !pc.state.AmChoking {
		pc.mu.Unlock()
		return nil
	}

	pc.state.AmChoking = false
	pc.mu.Unlock()

	msg := &Message{Type: MsgUnchoke}

	return pc.SendMessage(msg)
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

// SendCancel sends a cancel message for a block request.
func (pc *PeerConn) SendCancel(pieceIndex, offset, length int) error {
	req := &RequestMessage{
		Index:  uint32(pieceIndex),
		Begin:  uint32(offset),
		Length: uint32(length),
	}

	msg := &Message{
		Type:    MsgCancel,
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

// HandleMessage processes an incoming message and updates state.
func (pc *PeerConn) HandleMessage(msg *Message) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	switch msg.Type {
	case MsgChoke:
		pc.state.PeerChoking = true
	case MsgUnchoke:
		pc.state.PeerChoking = false
	case MsgInterested:
		pc.state.PeerInterested = true
	case MsgNotInterested:
		pc.state.PeerInterested = false
	case MsgHave:
		if len(msg.Payload) != 4 {
			return // Malformed message
		}

		pieceIndex := int(binary.BigEndian.Uint32(msg.Payload))
		if pc.bitfield != nil {
			pc.bitfield.SetPiece(pieceIndex)
		}
	}
}

// IsChoked returns true if the peer is choking us.
func (pc *PeerConn) IsChoked() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.state.PeerChoking
}

// AddDownloaded updates the download counter for rate calculation.
func (pc *PeerConn) AddDownloaded(amount int) {
	atomic.AddInt64(&pc.downloadedSinceLastReview, int64(amount))
	pc.lastBlockReceived = time.Now()
}

// AddUploaded updates the upload counter for rate calculation.
func (pc *PeerConn) AddUploaded(amount int) {
	atomic.AddInt64(&pc.uploadedSinceLastReview, int64(amount))
}

// CalculateRates calculates and resets the per-interval rates.
func (pc *PeerConn) CalculateRates(interval time.Duration) {
	seconds := interval.Seconds()
	if seconds == 0 {
		return
	}

	downloaded := atomic.SwapInt64(&pc.downloadedSinceLastReview, 0)
	pc.downloadRate = int64(float64(downloaded) / seconds)

	uploaded := atomic.SwapInt64(&pc.uploadedSinceLastReview, 0)
	pc.uploadRate = int64(float64(uploaded) / seconds)
}

// AddPendingRequest tracks a block we requested from this peer.
func (pc *PeerConn) AddPendingRequest(pieceIndex, offset, length int) {
	pc.pendingMu.Lock()
	defer pc.pendingMu.Unlock()

	if _, ok := pc.pendingRequests[pieceIndex]; !ok {
		pc.pendingRequests[pieceIndex] = make(map[int]time.Time)
	}

	pc.pendingRequests[pieceIndex][offset] = time.Now()
}

// RemovePendingRequest untracks a block we have received.
func (pc *PeerConn) RemovePendingRequest(pieceIndex, offset int) {
	pc.pendingMu.Lock()
	defer pc.pendingMu.Unlock()

	if _, ok := pc.pendingRequests[pieceIndex]; ok {
		delete(pc.pendingRequests[pieceIndex], offset)

		if len(pc.pendingRequests[pieceIndex]) == 0 {
			delete(pc.pendingRequests, pieceIndex)
		}
	}
}

// IsSnubbed checks if we are being snubbed by the peer.
func (pc *PeerConn) IsSnubbed(snubTimeout time.Duration) bool {
	pc.pendingMu.Lock()
	defer pc.pendingMu.Unlock()

	if len(pc.pendingRequests) == 0 {
		return false // Not waiting for anything, so not snubbed.
	}

	return !pc.lastBlockReceived.IsZero() && time.Since(pc.lastBlockReceived) > snubTimeout
}

// Close closes the peer connection.
func (pc *PeerConn) Close() error {
	return pc.conn.Close()
}
