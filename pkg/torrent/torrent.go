package torrent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

const (
	snubTimeout = 60 * time.Second
	// How many block requests to keep in flight to a single peer.
	requestPipelineSize = 5
	// How often to send PEX messages.
	pexInterval = 60 * time.Second
	// How often to run DHT announce/discovery.
	dhtInterval = 5 * time.Minute
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
	choker       *choker
	dht          *DHT
	state        TorrentState
	stats        TorrentStats
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	maxPeers     int
	port         uint16
	inEndgame    bool
}

// TorrentOptions contains options for creating a torrent.
type TorrentOptions struct {
	Metainfo       *Metainfo
	SavePath       string
	Port           uint16
	MaxPeers       int
	PickerStrategy PiecePickerStrategy
	UseDHT         bool
}

// NewTorrent creates a new torrent instance.
func NewTorrent(opts TorrentOptions) (*Torrent, error) {
	peerID, _ := newID()
	pieceManager := NewPieceManager(opts.Metainfo)
	piecePicker := NewPiecePicker(pieceManager, opts.PickerStrategy)
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
	t.choker = newChoker(t)

	for _, announceURL := range opts.Metainfo.GetAnnounceURLs() {
		tracker, err := createTracker(announceURL)
		if err == nil {
			t.trackers = append(t.trackers, tracker)
		}
	}

	if opts.UseDHT {
		dht, err := NewDHT(opts.Port)
		if err == nil {
			t.dht = dht
			t.dht.AddTorrent(t)
		}
	}

	return t, nil
}

// Start begins downloading the torrent.
func (t *Torrent) Start() error {
	t.mu.Lock()

	if t.state != TorrentStateIdle && t.state != TorrentStatePaused {
		t.mu.Unlock()
		return errors.New("torrent already active")
	}

	t.state = TorrentStateDownloading
	t.mu.Unlock()

	go t.announceLoop()
	go t.antiSnubLoop()
	go t.choker.start()
	go t.pexLoop()

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
	t.cancel()
	t.choker.stopChoker()

	if t.dht != nil {
		t.dht.Close()
	}

	t.mu.Lock()

	for _, peer := range t.peers {
		peer.Close()
	}

	t.peers = make(map[string]*PeerConn)
	t.mu.Unlock()

	return nil
}

// announceLoop periodically announces to trackers.
func (t *Torrent) announceLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	t.announce("started")

	for {
		select {
		case <-t.ctx.Done():
			t.announce("stopped")
			return
		case <-ticker.C:
			t.announce("")
		}
	}
}

// antiSnubLoop periodically checks for and handles snubbed peers.
func (t *Torrent) antiSnubLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.handleSnubbedPeers()
		}
	}
}

// dhtLoop manages DHT operations.
func (t *Torrent) dhtLoop() {
	ticker := time.NewTicker(dhtInterval)
	defer ticker.Stop()

	// Initial announce
	t.dht.Announce(t.metainfo.InfoHash(), t.port)

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.dht.Announce(t.metainfo.InfoHash(), t.port)
		}
	}
}

// pexLoop periodically sends PEX messages to eligible peers.
func (t *Torrent) pexLoop() {
	ticker := time.NewTicker(pexInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.broadcastPEX()
		}
	}
}

// broadcastPEX sends a PEX message with our current peer list to all PEX-enabled peers.
func (t *Torrent) broadcastPEX() {
	var currentPeers []Peer

	for _, p := range t.getPeers() {
		if p.listeningPort > 0 {
			currentPeers = append(currentPeers, Peer{IP: p.peer.IP, Port: p.listeningPort})
		}
	}

	if len(currentPeers) == 0 {
		return
	}

	for _, p := range t.getPeers() {
		if p.utPexID != 0 {
			p.SendPEX(currentPeers, nil) // Leaving dropped empty for simplicity
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
	}

	for _, tracker := range t.trackers {
		go func(tr TrackerClient) {
			ctx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
			defer cancel()

			resp, err := tr.Announce(ctx, req)
			if err != nil {
				return
			}

			t.addPeers(resp.Peers)
		}(tracker)
	}
}

// addPeers adds new peers to connect to.
func (t *Torrent) addPeers(peers []Peer) {
	for _, peer := range peers {
		if peer.IP.IsLoopback() && peer.Port == t.port {
			continue
		}

		peerKey := fmt.Sprintf("%s:%d", peer.IP, peer.Port)

		t.mu.RLock()
		_, exists := t.peers[peerKey]
		peerCount := len(t.peers)
		t.mu.RUnlock()

		if !exists && peerCount < t.maxPeers {
			go t.connectToPeer(peer)
		}
	}
}

// connectToPeer establishes a connection to a peer.
func (t *Torrent) connectToPeer(peer Peer) {
	pc, err := NewPeerConn(peer, t.metainfo.InfoHash(), t.peerID)
	if err != nil {
		return
	}

	_, err = pc.Handshake()
	if err != nil {
		pc.Close()
		return
	}

	if pc.supportsExtended {
		err := pc.SendExtendedHandshake(t.port)
		if err != nil {
			pc.Close()
			return
		}
	}

	peerKey := fmt.Sprintf("%s:%d", peer.IP, peer.Port)

	t.mu.Lock()
	t.peers[peerKey] = pc
	t.mu.Unlock()

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

	err := pc.SendBitfield(t.pieceManager.verified)
	if err != nil {
		return
	}

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		msg, err := pc.ReadMessage(2 * time.Minute)
		if err != nil {
			return
		}

		if msg == nil { // Keep-alive
			continue
		}

		err = t.handlePeerMessage(pc, msg)
		if err != nil {
			return
		}
	}
}

// handlePeerMessage processes a message from a peer.
func (t *Torrent) handlePeerMessage(pc *PeerConn, msg *Message) error {
	pc.HandleMessage(msg)

	switch msg.Type {
	case MsgBitfield:
		bf, err := NewBitfieldFromBytes(msg.Payload, t.pieceManager.totalPieces)
		if err != nil {
			return err
		}

		pc.bitfield = bf
		t.piecePicker.UpdateAvailability(bf)

		if t.isPeerInteresting(bf) {
			return pc.SendInterested()
		}

	case MsgHave:
		if !pc.state.AmInterested && !t.pieceManager.verified.HasPiece(int(msg.Type)) {
			return pc.SendInterested()
		}

	case MsgUnchoke:
		go t.requestPieces(pc)

	case MsgPiece:
		pieceMsg, err := ParsePiece(msg.Payload)
		if err != nil {
			return err
		}

		pc.AddDownloaded(len(pieceMsg.Block))
		t.mu.Lock()
		t.stats.Downloaded += int64(len(pieceMsg.Block))
		t.mu.Unlock()
		pc.RemovePendingRequest(int(pieceMsg.Index), int(pieceMsg.Begin))

		return t.handlePieceData(pieceMsg)

	case MsgRequest:
		req, err := ParseRequest(msg.Payload)
		if err != nil {
			return err
		}

		go t.handleRequest(pc, req)

	case MsgExtended:
		return t.handleExtendedMessage(pc, msg.Payload)
	}

	return nil
}

// handleExtendedMessage processes an extended protocol message.
func (t *Torrent) handleExtendedMessage(pc *PeerConn, payload []byte) error {
	if len(payload) == 0 {
		return errors.New("empty extended message")
	}

	extendedID := payload[0]
	payload = payload[1:]

	switch extendedID {
	case ExtendedHandshakeID:
		extHandshake, err := ParseExtendedHandshake(payload)
		if err != nil {
			return err
		}

		pc.mu.Lock()

		if pexID, ok := extHandshake.M["ut_pex"]; ok {
			pc.utPexID = uint8(pexID)
		}

		if extHandshake.P > 0 {
			pc.listeningPort = extHandshake.P
		}

		pc.mu.Unlock()

	default:
		pc.mu.RLock()
		utPexID := pc.utPexID
		pc.mu.RUnlock()

		if utPexID != 0 && extendedID == utPexID {
			return t.handlePexMessage(payload)
		}
	}

	return nil
}

// handlePexMessage parses a PEX message and adds the new peers.
func (t *Torrent) handlePexMessage(payload []byte) error {
	decoded, _, err := bencode.Decode(payload)
	if err != nil {
		return fmt.Errorf("failed to decode PEX message: %w", err)
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return errors.New("pex payload is not a dictionary")
	}

	var newPeers []Peer

	if addedVal, ok := dict["added"]; ok {
		if addedBytes, ok := addedVal.([]byte); ok {
			if len(addedBytes)%6 != 0 {
				return errors.New("invalid pex added length")
			}

			for i := 0; i < len(addedBytes); i += 6 {
				ip := net.IP(addedBytes[i : i+4])

				port := binary.BigEndian.Uint16(addedBytes[i+4 : i+6])
				if port > 0 {
					newPeers = append(newPeers, Peer{IP: ip, Port: port})
				}
			}
		}
	}

	if len(newPeers) > 0 {
		t.addPeers(newPeers)
	}

	return nil
}

// handleRequest serves a block to a peer if we are not choking them.
func (t *Torrent) handleRequest(pc *PeerConn, req *RequestMessage) {
	pc.mu.RLock()
	isChoking := pc.state.AmChoking
	pc.mu.RUnlock()

	if isChoking {
		return
	}

	pieceIndex := int(req.Index)
	if !t.pieceManager.verified.HasPiece(pieceIndex) {
		return
	}

	piece, err := t.pieceManager.GetPiece(pieceIndex)
	if err != nil {
		return
	}

	data, err := piece.ReadBlock(int(req.Begin), int(req.Length))
	if err != nil {
		return
	}

	err = pc.SendPiece(pieceIndex, int(req.Begin), data)
	if err == nil {
		pc.AddUploaded(len(data))
		t.mu.Lock()
		t.stats.Uploaded += int64(len(data))
		t.mu.Unlock()
	}
}

// requestPieces requests pieces from an unchoked peer (normal mode only).
func (t *Torrent) requestPieces(pc *PeerConn) {
	if t.inEndgame {
		return
	}

	for {
		pc.pendingMu.Lock()
		pipelineSize := len(pc.pendingRequests)
		pc.pendingMu.Unlock()

		if pc.IsChoked() || pipelineSize >= requestPipelineSize {
			return
		}

		pieceIndex, ok := t.piecePicker.PickPiece(pc.bitfield)
		if !ok {
			return
		}

		piece, err := t.pieceManager.GetPiece(pieceIndex)
		if err != nil {
			t.piecePicker.MarkFailed(pieceIndex)
			continue
		}

		for _, block := range piece.GetMissingBlocks() {
			pc.AddPendingRequest(pieceIndex, block.Offset, block.Length)

			err := pc.SendRequest(pieceIndex, block.Offset, block.Length)
			if err != nil {
				pc.RemovePendingRequest(pieceIndex, block.Offset)
				return
			}

			pc.pendingMu.Lock()
			pipelineSize = len(pc.pendingRequests)
			pc.pendingMu.Unlock()

			if pipelineSize >= requestPipelineSize {
				return
			}
		}
	}
}

// handlePieceData processes received piece data.
func (t *Torrent) handlePieceData(pieceMsg *PieceMessage) error {
	if t.inEndgame {
		t.broadcastCancel(int(pieceMsg.Index), int(pieceMsg.Begin), len(pieceMsg.Block))
	}

	piece, err := t.pieceManager.GetPiece(int(pieceMsg.Index))
	if err != nil {
		return err
	}

	err = piece.AddBlock(int(pieceMsg.Begin), pieceMsg.Block)
	if err != nil {
		return err
	}

	if piece.IsComplete() {
		if piece.Verify() {
			t.pieceManager.MarkVerified(int(pieceMsg.Index))
			t.piecePicker.MarkComplete(int(pieceMsg.Index))
			t.broadcastHave(int(pieceMsg.Index))

			if !t.inEndgame && t.shouldEnterEndgame() {
				t.mu.Lock()
				t.inEndgame = true
				t.mu.Unlock()

				go t.broadcastEndgameRequests()
			}
		} else {
			piece.Reset()
			t.piecePicker.MarkFailed(int(pieceMsg.Index))
		}
	}

	return nil
}

// shouldEnterEndgame checks if the conditions for endgame mode are met.
func (t *Torrent) shouldEnterEndgame() bool {
	return !t.pieceManager.IsComplete() &&
		(t.pieceManager.verified.Count()+t.piecePicker.InProgressCount() == t.pieceManager.totalPieces)
}

// broadcastEndgameRequests sends requests for all missing blocks to all available peers.
func (t *Torrent) broadcastEndgameRequests() {
	missing := t.pieceManager.GetAllMissingBlocks()
	peers := t.getPeers()

	for _, block := range missing {
		for _, peer := range peers {
			if peer.bitfield != nil && peer.bitfield.HasPiece(block.PieceIndex) && !peer.IsChoked() {
				peer.SendRequest(block.PieceIndex, block.Offset, block.Length)
			}
		}
	}
}

// broadcastCancel sends a cancel message for a specific block to all peers.
func (t *Torrent) broadcastCancel(pieceIndex, offset, length int) {
	for _, peer := range t.getPeers() {
		peer.SendCancel(pieceIndex, offset, length)
	}
}

// handleSnubbedPeers finds snubbed peers and retries downloading their blocks.
func (t *Torrent) handleSnubbedPeers() {
	for _, pc := range t.getPeers() {
		if pc.IsSnubbed(snubTimeout) {
			pc.pendingMu.Lock()

			for pieceIndex, blocks := range pc.pendingRequests {
				for offset := range blocks {
					t.piecePicker.MarkFailed(pieceIndex)
					delete(pc.pendingRequests[pieceIndex], offset)
				}

				delete(pc.pendingRequests, pieceIndex)
			}

			pc.pendingMu.Unlock()
		}
	}
}

// broadcastHave sends have messages to all peers.
func (t *Torrent) broadcastHave(pieceIndex int) {
	for _, pc := range t.getPeers() {
		pc.SendHave(pieceIndex)
	}
}

// isPeerInteresting checks if a peer has pieces we need.
func (t *Torrent) isPeerInteresting(peerBitfield *Bitfield) bool {
	return !t.pieceManager.IsComplete() &&
		peerBitfield != nil && peerBitfield.Count() > 0
}

func (t *Torrent) chokePeer(p *PeerConn)   { p.SendChoke() }
func (t *Torrent) unchokePeer(p *PeerConn) { p.SendUnchoke() }
func (t *Torrent) getPeers() []*PeerConn {
	t.mu.RLock()
	defer t.mu.RUnlock()

	peers := make([]*PeerConn, 0, len(t.peers))
	for _, p := range t.peers {
		peers = append(peers, p)
	}

	return peers
}

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
