package torrent

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

const (
	alpha = 3 // Concurrency factor for lookups
)

// DHT represents a DHT server and client.
type DHT struct {
	nodeID       [20]byte
	conn         *net.UDPConn
	routingTable *routingTable
	peerStore    map[[20]byte][]Peer
	storeMu      sync.RWMutex
	transactions *transactionManager
	torrents     map[[20]byte]*Torrent
	torrentsMu   sync.RWMutex
	tokenSecrets [2]string // [0] is current, [1] is previous
	tokenMu      sync.RWMutex
	stop         chan struct{}
}

// NewDHT creates and starts a new DHT server.
func NewDHT(port uint16) (*DHT, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate node ID: %w", err)
	}

	dht := &DHT{
		nodeID:       id,
		conn:         conn,
		peerStore:    make(map[[20]byte][]Peer),
		transactions: newTransactionManager(),
		torrents:     make(map[[20]byte]*Torrent),
		stop:         make(chan struct{}),
	}
	dht.routingTable = newRoutingTable(id, dht)
	dht.tokenSecrets[0], _ = dht.generateTokenSecret()
	dht.tokenSecrets[1] = dht.tokenSecrets[0]

	go dht.serve()
	go dht.bootstrap()
	go dht.rotateTokenSecrets()

	return dht, nil
}

// serve is the main loop that listens for and handles incoming UDP packets.
func (d *DHT) serve() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-d.stop:
			return
		default:
			d.conn.SetReadDeadline(time.Now().Add(time.Second))
			n, addr, err := d.conn.ReadFromUDP(buf)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) {
					continue
				}
				return // Connection closed
			}
			go d.handleMessage(addr, buf[:n])
		}
	}
}

// bootstrap introduces the client to the DHT network.
func (d *DHT) bootstrap() {
	bootstrapNodes := []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
	}

	for _, addrStr := range bootstrapNodes {
		go func(addr string) {
			host, portStr, _ := net.SplitHostPort(addr)
			ips, err := net.LookupIP(host)
			if err != nil || len(ips) == 0 {
				return
			}
			port, _ := strconv.Atoi(portStr)
			udpAddr := &net.UDPAddr{IP: ips[0], Port: port}
			d.findNode(udpAddr, d.nodeID, nil) // No callback needed for initial bootstrap
		}(addrStr)
	}
}

// AddTorrent registers a torrent with the DHT to be announced.
func (d *DHT) AddTorrent(t *Torrent) {
	d.torrentsMu.Lock()
	defer d.torrentsMu.Unlock()
	d.torrents[t.metainfo.InfoHash()] = t
}

// Announce finds peers for a torrent and announces our presence.
func (d *DHT) Announce(infoHash [20]byte, port uint16) {
	go newLookup(d, infoHash, port).run()
}

// Close shuts down the DHT client.
func (d *DHT) Close() error {
	close(d.stop)
	return d.conn.Close()
}

// --- KRPC Message Handling ---

func (d *DHT) handleMessage(addr *net.UDPAddr, data []byte) {
	decoded, _, err := bencode.Decode(data)
	if err != nil {
		return
	}
	msg, ok := decoded.(map[string]any)
	if !ok {
		return
	}

	y, _ := msg["y"].([]byte)
	switch string(y) {
	case "q":
		d.handleQuery(addr, msg)
	case "r", "e":
		d.transactions.handleResponse(msg)
	}
}

func (d *DHT) handleQuery(addr *net.UDPAddr, msg map[string]any) {
	qBytes, _ := msg["q"].([]byte)
	q := string(qBytes)
	a, _ := msg["a"].(map[string]any)
	idBytes, _ := a["id"].([]byte)
	var id [20]byte
	copy(id[:], idBytes)

	// All queries imply the sender is a valid node.
	d.routingTable.Add(&Node{ID: id, Addr: addr, lastSeen: time.Now()})

	tid, ok := msg["t"].([]byte)
	if !ok {
		return // No transaction ID, cannot reply.
	}

	switch q {
	case "ping":
		d.sendPong(addr, tid)
	case "find_node":
		targetBytes, _ := a["target"].([]byte)
		var target [20]byte
		copy(target[:], targetBytes)
		d.sendFindNodeResponse(addr, tid, target)
	case "get_peers":
		infoHashBytes, _ := a["info_hash"].([]byte)
		var infoHash [20]byte
		copy(infoHash[:], infoHashBytes)
		d.sendGetPeersResponse(addr, tid, infoHash)
	case "announce_peer":
		infoHashBytes, _ := a["info_hash"].([]byte)
		portVal, _ := a["port"].(int64)
		tokenBytes, _ := a["token"].([]byte)
		var infoHash [20]byte
		copy(infoHash[:], infoHashBytes)

		if d.validateToken(string(tokenBytes), addr.IP) {
			d.storePeer(infoHash, Peer{IP: addr.IP, Port: uint16(portVal)})
		}
		d.sendAnnouncePeerResponse(addr, tid)
	}
}

// --- Query Implementations ---

func (d *DHT) ping(addr *net.UDPAddr, cb func(error)) {
	tid := d.transactions.new(func(msg map[string]any, err error) {
		if cb != nil {
			cb(err)
		}
	})
	msg := map[string]any{
		"t": tid, "y": "q", "q": "ping",
		"a": map[string]any{"id": d.nodeID[:]},
	}
	d.send(addr, msg)
}

func (d *DHT) findNode(addr *net.UDPAddr, target [20]byte, cb func(msg map[string]any, err error)) {
	tid := d.transactions.new(cb)
	msg := map[string]any{
		"t": tid, "y": "q", "q": "find_node",
		"a": map[string]any{"id": d.nodeID[:], "target": target[:]},
	}
	d.send(addr, msg)
}

func (d *DHT) findPeers(addr *net.UDPAddr, infoHash [20]byte, cb func(msg map[string]any, err error)) {
	tid := d.transactions.new(cb)
	msg := map[string]any{
		"t": tid, "y": "q", "q": "get_peers",
		"a": map[string]any{"id": d.nodeID[:], "info_hash": infoHash[:]},
	}
	d.send(addr, msg)
}

func (d *DHT) announce(addr *net.UDPAddr, infoHash [20]byte, port uint16, token string, cb func(msg map[string]any, err error)) {
	tid := d.transactions.new(cb)
	msg := map[string]any{
		"t": tid, "y": "q", "q": "announce_peer",
		"a": map[string]any{
			"id":        d.nodeID[:],
			"info_hash": infoHash[:],
			"port":      int(port),
			"token":     token,
		},
	}
	d.send(addr, msg)
}

// --- Response Implementations ---

func (d *DHT) sendPong(addr *net.UDPAddr, tid []byte) {
	d.send(addr, map[string]any{
		"t": tid, "y": "r", "r": map[string]any{"id": d.nodeID[:]},
	})
}

func (d *DHT) sendFindNodeResponse(addr *net.UDPAddr, tid []byte, target [20]byte) {
	nodes := d.routingTable.findClosest(target, k)
	d.send(addr, map[string]any{
		"t": tid, "y": "r",
		"r": map[string]any{
			"id":    d.nodeID[:],
			"nodes": compactNodes(nodes),
		},
	})
}

func (d *DHT) sendGetPeersResponse(addr *net.UDPAddr, tid []byte, infoHash [20]byte) {
	d.storeMu.RLock()
	peers, found := d.peerStore[infoHash]
	d.storeMu.RUnlock()

	token := d.generateToken(addr.IP)
	resp := map[string]any{
		"id":    d.nodeID[:],
		"token": token,
	}

	if found && len(peers) > 0 {
		var values []string
		for _, p := range peers {
			values = append(values, p.Compact())
		}
		resp["values"] = values
	} else {
		nodes := d.routingTable.findClosest(infoHash, k)
		resp["nodes"] = compactNodes(nodes)
	}

	d.send(addr, map[string]any{"t": tid, "y": "r", "r": resp})
}

func (d *DHT) sendAnnouncePeerResponse(addr *net.UDPAddr, tid []byte) {
	d.sendPong(addr, tid)
}

// storePeer saves a peer's contact info for a given infohash.
func (d *DHT) storePeer(infoHash [20]byte, peer Peer) {
	d.storeMu.Lock()
	// Limit stored peers per infohash
	if len(d.peerStore[infoHash]) < 50 {
		d.peerStore[infoHash] = append(d.peerStore[infoHash], peer)
	}
	d.storeMu.Unlock()

	// Also notify the torrent if it exists
	d.torrentsMu.RLock()
	t, ok := d.torrents[infoHash]
	d.torrentsMu.RUnlock()
	if ok {
		go t.addPeers([]Peer{peer})
	}
}

// send encodes and sends a KRPC message.
func (d *DHT) send(addr *net.UDPAddr, msg map[string]any) {
	data, err := bencode.Encode(msg)
	if err != nil {
		return
	}
	d.conn.WriteToUDP(data, addr)
}

// --- Transaction Manager ---.
type transactionManager struct {
	m   sync.Mutex
	id  uint16
	cbs map[string]func(map[string]any, error)
}

func newTransactionManager() *transactionManager {
	return &transactionManager{cbs: make(map[string]func(map[string]any, error))}
}

func (tm *transactionManager) new(cb func(map[string]any, error)) []byte {
	tm.m.Lock()
	defer tm.m.Unlock()
	tm.id++
	tid := make([]byte, 2)
	binary.BigEndian.PutUint16(tid, tm.id)
	if cb != nil {
		tm.cbs[string(tid)] = cb
		time.AfterFunc(15*time.Second, func() {
			tm.m.Lock()
			if cb, ok := tm.cbs[string(tid)]; ok {
				delete(tm.cbs, string(tid))
				tm.m.Unlock()
				cb(nil, errors.New("transaction timeout"))
			} else {
				tm.m.Unlock()
			}
		})
	}
	return tid
}

func (tm *transactionManager) handleResponse(msg map[string]any) {
	tidBytes, ok := msg["t"].([]byte)
	if !ok {
		return
	}
	tid := string(tidBytes)
	tm.m.Lock()
	cb, ok := tm.cbs[tid]
	if ok {
		delete(tm.cbs, tid)
	}
	tm.m.Unlock()

	if !ok {
		return
	}

	if y, _ := msg["y"].([]byte); string(y) == "e" {
		errList, _ := msg["e"].([]any)
		errStr := "dht error"
		if len(errList) > 1 {
			if s, ok := errList[1].([]byte); ok {
				errStr = string(s)
			}
		}
		cb(nil, errors.New(errStr))
	} else {
		cb(msg, nil)
	}
}

// --- Token Management ---.
func (d *DHT) generateTokenSecret() (string, error) {
	b := make([]byte, 20)
	_, err := rand.Read(b)
	return string(b), err
}

func (d *DHT) rotateTokenSecrets() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.tokenMu.Lock()
			d.tokenSecrets[1] = d.tokenSecrets[0]
			d.tokenSecrets[0], _ = d.generateTokenSecret()
			d.tokenMu.Unlock()
		}
	}
}

func (d *DHT) generateToken(ip net.IP) string {
	d.tokenMu.RLock()
	secret := d.tokenSecrets[0]
	d.tokenMu.RUnlock()
	h := sha1.New()
	h.Write(ip)
	h.Write([]byte(secret))
	return string(h.Sum(nil))
}

func (d *DHT) validateToken(token string, ip net.IP) bool {
	d.tokenMu.RLock()
	secret1 := d.tokenSecrets[0]
	secret2 := d.tokenSecrets[1]
	d.tokenMu.RUnlock()

	h1 := sha1.New()
	h1.Write(ip)
	h1.Write([]byte(secret1))
	if token == string(h1.Sum(nil)) {
		return true
	}

	h2 := sha1.New()
	h2.Write(ip)
	h2.Write([]byte(secret2))
	return token == string(h2.Sum(nil))
}

// --- Iterative Lookup ---

type lookup struct {
	dht      *DHT
	target   [20]byte
	port     uint16
	queried  map[string]bool
	nodes    []*Node
	mu       sync.Mutex
	inflight int
}

func newLookup(dht *DHT, target [20]byte, port uint16) *lookup {
	return &lookup{
		dht:     dht,
		target:  target,
		port:    port,
		queried: make(map[string]bool),
		nodes:   dht.routingTable.findClosest(target, k),
	}
}

func (l *lookup) run() {
	l.mu.Lock()
	initialNodes := l.nodes
	l.mu.Unlock()

	if len(initialNodes) == 0 {
		l.dht.bootstrap()
		return
	}

	for _, n := range initialNodes {
		l.query(n)
	}
}

func (l *lookup) query(node *Node) {
	l.mu.Lock()
	if l.queried[node.Addr.String()] {
		l.mu.Unlock()
		return
	}
	l.queried[node.Addr.String()] = true
	l.inflight++
	l.mu.Unlock()

	l.dht.findPeers(node.Addr, l.target, func(msg map[string]any, err error) {
		l.mu.Lock()
		l.inflight--
		l.mu.Unlock()

		if err != nil || msg == nil {
			return
		}

		r, ok := msg["r"].(map[string]any)
		if !ok {
			return
		}

		if newNodesBytes, ok := r["nodes"].([]byte); ok {
			newNodes, _ := decodeCompactNodes(newNodesBytes)
			for _, n := range newNodes {
				l.dht.routingTable.Add(n)
			}
			l.nodes = append(l.nodes, newNodes...)
		}

		if peers, ok := r["values"].([]any); ok {
			var foundPeers []Peer
			for _, pAny := range peers {
				if pStr, ok := pAny.(string); ok && len(pStr) == 6 {
					foundPeers = append(foundPeers, Peer{
						IP:   net.IP(pStr[0:4]),
						Port: binary.BigEndian.Uint16([]byte(pStr[4:6])),
					})
				}
			}
			l.dht.torrentsMu.RLock()
			t, tFound := l.dht.torrents[l.target]
			l.dht.torrentsMu.RUnlock()
			if tFound {
				t.addPeers(foundPeers)
			}
		}

		if tok, ok := r["token"].([]byte); ok {
			l.dht.announce(node.Addr, l.target, l.port, string(tok), nil)
		}

		l.mu.Lock()
		var sorted byDistance
		sorted.target = l.target
		sorted.nodes = l.nodes
		sort.Sort(sorted)
		l.nodes = sorted.nodes
		l.mu.Unlock()

		for i := 0; i < len(l.nodes) && l.inflight < alpha; i++ {
			l.query(l.nodes[i])
		}
	})
}
