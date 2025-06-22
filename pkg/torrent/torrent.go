package torrent

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/url"
	"sync"
	"time"
)

// TorrentState represents the state of a torrent.
type TorrentState int

const (
	TorrentStateIdle TorrentState = iota
	TorrentStateDownloading
	TorrentStateSeeding
	TorrentStatePaused
	TorrentStateError
	TorrentStateChecking
)

// TorrentStats contains torrent statistics.
type TorrentStats struct {
	State          TorrentState
	Progress       float64
	DownloadRate   int64
	UploadRate     int64
	Downloaded     int64
	Uploaded       int64
	PeersConnected int
	PeersTotal     int
	SeedsConnected int
	SeedsTotal     int
	TimeRemaining  time.Duration
}

// Torrent represents an active torrent.
type Torrent struct {
	metainfo     *Metainfo
	peerID       [20]byte
	pieceManager *PieceManager
	piecePicker  *PiecePicker
	peers        map[string]*PeerConn
	trackers     []TrackerClient
	dht          *DHT
	state        TorrentState
	stats        TorrentStats
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	maxPeers     int
	port         uint16
}

// TorrentOptions contains options for creating a torrent.
type TorrentOptions struct {
	Metainfo       *Metainfo
	SavePath       string
	Port           uint16
	MaxPeers       int
	UseDHT         bool
	PickerStrategy PiecePickerStrategy
}

// NewTorrent creates a new torrent instance.
func NewTorrent(opts TorrentOptions) (*Torrent, error) {
	// Generate peer ID
	peerID := generatePeerID()

	// Create piece manager
	pieceManager := NewPieceManager(opts.Metainfo)

	// Create piece picker
	piecePicker := NewPiecePicker(pieceManager, opts.PickerStrategy)

	// Setup context
	ctx, cancel := context.WithCancel(context.Background())

	t := &Torrent{
		metainfo:     opts.Metainfo,
		peerID:       peerID,
		pieceManager: pieceManager,
		piecePicker:  piecePicker,
		peers:        make(map[string]*PeerConn),
		trackers:     []TrackerClient{},
		state:        TorrentStateIdle,
		ctx:          ctx,
		cancel:       cancel,
		maxPeers:     opts.MaxPeers,
		port:         opts.Port,
	}

	// Initialize trackers
	for _, announceURL := range opts.Metainfo.GetAnnounceURLs() {
		tracker, err := createTracker(announceURL)
		if err != nil {
			continue
		}
		t.trackers = append(t.trackers, tracker)
	}

	// Initialize DHT if requested
	if opts.UseDHT {
		dht, err := NewDHT(peerID, opts.Port)
		if err == nil {
			t.dht = dht
		}
	}

	return t, nil
}

// Start begins downloading the torrent.
func (t *Torrent) Start() error {
	t.mu.Lock()
	if t.state != TorrentStateIdle && t.state != TorrentStatePaused {
		t.mu.Unlock()
		return fmt.Errorf("torrent already active")
	}
	t.state = TorrentStateDownloading
	t.mu.Unlock()

	// Start tracker announcer
	go t.announceLoop()

	// Start peer manager
	go t.peerLoop()

	// Start DHT if enabled
	if t.dht != nil {
		go t.dhtLoop()
	}

	return nil
}

// Stop stops the torrent.
func (t *Torrent) Stop() error {
	t.mu.Lock()
	t.state = TorrentStateIdle
	t.mu.Unlock()

	// Cancel context
	t.cancel()

	// Close all peer connections
	t.mu.Lock()
	for _, peer := range t.peers {
		peer.Close()
	}
	t.peers = make(map[string]*PeerConn)
	t.mu.Unlock()

	// Close DHT
	if t.dht != nil {
		t.dht.Close()
	}

	return nil
}

// Pause pauses the torrent.
func (t *Torrent) Pause() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state != TorrentStateDownloading && t.state != TorrentStateSeeding {
		return fmt.Errorf("torrent not active")
	}

	t.state = TorrentStatePaused
	return nil
}

// Resume resumes the torrent.
func (t *Torrent) Resume() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state != TorrentStatePaused {
		return fmt.Errorf("torrent not paused")
	}

	if t.pieceManager.verified.IsComplete() {
		t.state = TorrentStateSeeding
	} else {
		t.state = TorrentStateDownloading
	}

	return nil
}

// GetStats returns current torrent statistics.
func (t *Torrent) GetStats() TorrentStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := t.stats
	stats.State = t.state
	stats.Progress = t.pieceManager.Progress()
	stats.PeersConnected = len(t.peers)

	return stats
}

// GetPieceStates returns the state of all pieces.
func (t *Torrent) GetPieceStates() []PieceState {
	states := make([]PieceState, t.pieceManager.totalPieces)

	for i := 0; i < t.pieceManager.totalPieces; i++ {
		piece, _ := t.pieceManager.GetPiece(i)
		states[i] = piece.State
	}

	return states
}

// announceLoop periodically announces to trackers.
func (t *Torrent) announceLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	// Initial announce
	t.announce("started")

	for {
		select {
		case <-t.ctx.Done():
			// Final announce
			t.announce("stopped")
			return
		case <-ticker.C:
			t.announce("")
		}
	}
}

// announce sends announce requests to all trackers.
func (t *Torrent) announce(event string) {
	req := &AnnounceRequest{
		InfoHash:   t.metainfo.InfoHash(),
		PeerID:     t.peerID,
		Port:       t.port,
		Uploaded:   t.stats.Uploaded,
		Downloaded: t.stats.Downloaded,
		Left:       t.metainfo.TotalSize() - t.stats.Downloaded,
		Event:      event,
		NumWant:    50,
		Compact:    true,
	}

	for _, tracker := range t.trackers {
		go func(tr TrackerClient) {
			ctx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
			defer cancel()

			resp, err := tr.Announce(ctx, req)
			if err != nil {
				return
			}

			// Add peers
			t.addPeers(resp.Peers)
		}(tracker)
	}
}

// addPeers adds new peers to connect to.
func (t *Torrent) addPeers(peers []Peer) {
	for _, peer := range peers {
		peerKey := fmt.Sprintf("%s:%d", peer.IP, peer.Port)

		t.mu.RLock()
		_, exists := t.peers[peerKey]
		peerCount := len(t.peers)
		t.mu.RUnlock()

		if exists || peerCount >= t.maxPeers {
			continue
		}

		// Try to connect to peer
		go t.connectToPeer(peer)
	}
}

// connectToPeer establishes a connection to a peer.
func (t *Torrent) connectToPeer(peer Peer) {
	pc, err := NewPeerConn(peer, t.metainfo.InfoHash(), t.peerID)
	if err != nil {
		return
	}

	// Perform handshake
	if err := pc.Handshake(); err != nil {
		pc.Close()
		return
	}

	// Add to peer list
	peerKey := fmt.Sprintf("%s:%d", peer.IP, peer.Port)
	t.mu.Lock()
	t.peers[peerKey] = pc
	t.mu.Unlock()

	// Handle peer connection
	go t.handlePeer(pc, peerKey)
}

// handlePeer manages communication with a peer.
func (t *Torrent) handlePeer(pc *PeerConn, peerKey string) {
	defer func() {
		pc.Close()
		t.mu.Lock()
		delete(t.peers, peerKey)
		t.mu.Unlock()
	}()

	// Send bitfield
	if err := pc.SendBitfield(t.pieceManager.verified); err != nil {
		return
	}

	// Main peer loop
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		// Read message
		msg, err := pc.ReadMessage(2 * time.Minute)
		if err != nil {
			return
		}

		if msg == nil {
			// Keep-alive
			continue
		}

		// Handle message
		if err := t.handlePeerMessage(pc, msg); err != nil {
			return
		}
	}
}

// handlePeerMessage processes a message from a peer.
func (t *Torrent) handlePeerMessage(pc *PeerConn, msg *Message) error {
	switch msg.Type {
	case MsgBitfield:
		// Initialize peer's bitfield
		bf, err := NewBitfieldFromBytes(msg.Payload, t.pieceManager.totalPieces)
		if err != nil {
			return err
		}
		pc.bitfield = bf
		t.piecePicker.UpdateAvailability(bf)

		// Express interest if peer has pieces we need
		if t.isPeerInteresting(bf) {
			return pc.SendInterested()
		}

	case MsgHave:
		// Update peer's bitfield
		if len(msg.Payload) != 4 {
			return fmt.Errorf("invalid have message")
		}
		pieceIndex := int(binary.BigEndian.Uint32(msg.Payload))

		if pc.bitfield != nil {
			pc.bitfield.SetPiece(pieceIndex)
		}

		// Check if we're now interested
		if !pc.state.AmInterested && !t.pieceManager.verified.HasPiece(pieceIndex) {
			return pc.SendInterested()
		}

	case MsgUnchoke:
		// Peer unchoked us, start requesting pieces
		pc.state.PeerChoking = false
		go t.requestPieces(pc)

	case MsgPiece:
		// Handle received piece
		piece, err := ParsePiece(msg.Payload)
		if err != nil {
			return err
		}

		return t.handlePieceData(piece)
	}

	// Let PeerConn handle state changes
	return pc.HandleMessage(msg)
}

// isPeerInteresting checks if peer has pieces we need.
func (t *Torrent) isPeerInteresting(peerBitfield *Bitfield) bool {
	for i := 0; i < t.pieceManager.totalPieces; i++ {
		if peerBitfield.HasPiece(i) && !t.pieceManager.verified.HasPiece(i) {
			return true
		}
	}
	return false
}

// requestPieces requests pieces from an unchoked peer.
func (t *Torrent) requestPieces(pc *PeerConn) {
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		// Check if still unchoked
		if pc.IsChoked() {
			return
		}

		// Pick a piece
		pieceIndex, ok := t.piecePicker.PickPiece(pc.bitfield)
		if !ok {
			// No pieces to download from this peer
			return
		}

		// Get piece
		piece, err := t.pieceManager.GetPiece(pieceIndex)
		if err != nil {
			continue
		}

		// Request missing blocks
		for _, block := range piece.GetMissingBlocks() {
			if err := pc.SendRequest(pieceIndex, block.Offset, block.Length); err != nil {
				return
			}
		}
	}
}

// handlePieceData processes received piece data.
func (t *Torrent) handlePieceData(pieceMsg *PieceMessage) error {
	piece, err := t.pieceManager.GetPiece(int(pieceMsg.Index))
	if err != nil {
		return err
	}

	// Add block to piece
	if err := piece.AddBlock(int(pieceMsg.Begin), pieceMsg.Block); err != nil {
		return err
	}

	// Check if piece is complete
	if piece.IsComplete() {
		// Verify piece
		if piece.Verify() {
			// Mark as verified
			t.pieceManager.MarkVerified(int(pieceMsg.Index))
			t.piecePicker.MarkComplete(int(pieceMsg.Index))

			// Announce to all peers
			t.broadcastHave(int(pieceMsg.Index))

			// Update stats
			t.mu.Lock()
			t.stats.Downloaded += piece.Length
			t.mu.Unlock()
		} else {
			// Piece failed verification, reset
			piece.Reset()
			t.piecePicker.MarkFailed(int(pieceMsg.Index))
		}
	}

	return nil
}

// broadcastHave sends have messages to all peers.
func (t *Torrent) broadcastHave(pieceIndex int) {
	t.mu.RLock()
	peers := make([]*PeerConn, 0, len(t.peers))
	for _, pc := range t.peers {
		peers = append(peers, pc)
	}
	t.mu.RUnlock()

	for _, pc := range peers {
		pc.SendHave(pieceIndex)
	}
}

// peerLoop manages peer connections.
func (t *Torrent) peerLoop() {
	// This would handle:
	// - Maintaining peer connections
	// - Handling timeouts
	// - Optimistic unchoking
	// - Connection limits
}

// dhtLoop manages DHT operations.
func (t *Torrent) dhtLoop() {
	if t.dht == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			// Search for peers via DHT
			peers, err := t.dht.FindPeers(t.ctx, t.metainfo.InfoHash())
			if err == nil {
				t.addPeers(peers)
			}
		}
	}
}

// Helper functions

// generatePeerID creates a peer ID.
func generatePeerID() [20]byte {
	var id [20]byte
	// Use Azureus-style peer ID: -TDM001-<12 random bytes>
	copy(id[:], "-TDM001-")
	rand.Read(id[8:])
	return id
}

// createTracker creates a tracker client based on URL scheme.
func createTracker(announceURL string) (TrackerClient, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		return NewHTTPTrackerClient(announceURL), nil
	case "udp":
		return NewUDPTrackerClient(announceURL)
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
