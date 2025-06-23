package torrent

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// Peer represents a peer in the swarm.
type Peer struct {
	IP   net.IP
	Port uint16
	ID   []byte
}

// Compact converts peer info into the 6-byte compact format.
func (p *Peer) Compact() string {
	buf := new(bytes.Buffer)
	buf.Write(p.IP.To4())
	binary.Write(buf, binary.BigEndian, p.Port)
	return buf.String()
}

// TrackerResponse contains the tracker's response.
type TrackerResponse struct {
	Interval   int
	Peers      []Peer
	Complete   int
	Incomplete int
}

// TrackerClient interface for different tracker types.
type TrackerClient interface {
	Announce(ctx context.Context, req *AnnounceRequest) (*TrackerResponse, error)
}

// AnnounceRequest contains announce parameters.
type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string
	NumWant    int
	Compact    bool
}

// HTTPTrackerClient implements HTTP/HTTPS tracker protocol.
type HTTPTrackerClient struct {
	announceURL string
	client      *http.Client
}

// NewHTTPTrackerClient creates a new HTTP tracker client.
func NewHTTPTrackerClient(announceURL string) *HTTPTrackerClient {
	return &HTTPTrackerClient{
		announceURL: announceURL,
		client: &http.Client{
			Timeout: 30 * time.Second, // Global timeout for the entire request
		},
	}
}

// Announce sends an announce request to the HTTP tracker with retry logic.
func (c *HTTPTrackerClient) Announce(ctx context.Context, req *AnnounceRequest) (*TrackerResponse, error) {
	var resp *TrackerResponse
	var err error

	err = retry(3, 5*time.Second, func() error {
		params := url.Values{}
		params.Set("info_hash", string(req.InfoHash[:]))
		params.Set("peer_id", string(req.PeerID[:]))
		params.Set("port", strconv.Itoa(int(req.Port)))
		params.Set("uploaded", strconv.FormatInt(req.Uploaded, 10))
		params.Set("downloaded", strconv.FormatInt(req.Downloaded, 10))
		params.Set("left", strconv.FormatInt(req.Left, 10))
		params.Set("compact", "1")

		if req.Event != "" {
			params.Set("event", req.Event)
		}
		if req.NumWant > 0 {
			params.Set("numwant", strconv.Itoa(req.NumWant))
		}

		fullURL := c.announceURL + "?" + params.Encode()
		httpReq, reqErr := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		if reqErr != nil {
			return reqErr // Don't retry on bad request creation
		}

		httpResp, httpErr := c.client.Do(httpReq)
		if httpErr != nil {
			return httpErr // Retry on network errors
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			return fmt.Errorf("tracker returned non-200 status: %s", httpResp.Status)
		}

		body, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return readErr
		}

		resp, err = parseHTTPTrackerResponse(body)
		return err
	})

	return resp, err
}

// parseHTTPTrackerResponse parses the bencode response from HTTP tracker.
func parseHTTPTrackerResponse(data []byte) (*TrackerResponse, error) {
	val, _, err := bencode.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode tracker response: %w", err)
	}

	dict, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tracker response is not a dictionary")
	}

	if failureReason, exists := dict["failure reason"]; exists {
		if reason, ok := failureReason.([]byte); ok {
			return nil, fmt.Errorf("tracker returned failure: %s", string(reason))
		}
	}

	resp := &TrackerResponse{}
	if interval, ok := dict["interval"].(int64); ok {
		resp.Interval = int(interval)
	}
	if complete, ok := dict["complete"].(int64); ok {
		resp.Complete = int(complete)
	}
	if incomplete, ok := dict["incomplete"].(int64); ok {
		resp.Incomplete = int(incomplete)
	}

	if peersVal, exists := dict["peers"]; exists {
		switch peers := peersVal.(type) {
		case []byte: // Compact format
			resp.Peers = parseCompactPeers(peers)
		case string: // Some trackers send it as a string
			resp.Peers = parseCompactPeers([]byte(peers))
		case []any: // Dictionary format
			resp.Peers = parseDictPeers(peers)
		}
	}

	return resp, nil
}

// parseCompactPeers parses compact peer format (6 bytes per peer).
func parseCompactPeers(data []byte) []Peer {
	if len(data)%6 != 0 {
		return nil
	}

	numPeers := len(data) / 6
	peers := make([]Peer, 0, numPeers)

	for i := 0; i < numPeers; i++ {
		offset := i * 6
		ip := net.IP(data[offset : offset+4])
		port := binary.BigEndian.Uint16(data[offset+4 : offset+6])

		peers = append(peers, Peer{
			IP:   ip,
			Port: port,
		})
	}

	return peers
}

// parseDictPeers parses dictionary peer format.
func parseDictPeers(peerList []any) []Peer {
	peers := make([]Peer, 0, len(peerList))

	for _, peerVal := range peerList {
		peerDict, ok := peerVal.(map[string]any)
		if !ok {
			continue
		}

		var peer Peer
		if ipVal, ok := peerDict["ip"].([]byte); ok {
			peer.IP = net.ParseIP(string(ipVal))
		}
		if portVal, ok := peerDict["port"].(int64); ok {
			peer.Port = uint16(portVal)
		}
		if idVal, ok := peerDict["peer id"].([]byte); ok {
			peer.ID = idVal
		}

		if peer.IP != nil && peer.Port > 0 {
			peers = append(peers, peer)
		}
	}

	return peers
}

// UDPTrackerClient implements UDP tracker protocol.
type UDPTrackerClient struct {
	announceURL string
	conn        net.Conn
}

// UDP tracker protocol constants
const (
	actionConnect  = 0
	actionAnnounce = 1
	actionScrape   = 2
	actionError    = 3
)

// NewUDPTrackerClient creates a new UDP tracker client.
func NewUDPTrackerClient(announceURL string) (*UDPTrackerClient, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}

	conn, err := net.Dial("udp", u.Host)
	if err != nil {
		return nil, err
	}

	return &UDPTrackerClient{
		announceURL: announceURL,
		conn:        conn,
	}, nil
}

// Announce sends an announce request to the UDP tracker.
func (c *UDPTrackerClient) Announce(ctx context.Context, req *AnnounceRequest) (*TrackerResponse, error) {
	var resp *TrackerResponse
	var err error

	err = retry(3, 15*time.Second, func() error {
		connID, innerErr := c.connect(ctx)
		if innerErr != nil {
			return innerErr
		}
		resp, innerErr = c.announce(ctx, connID, req)
		return innerErr
	})

	return resp, err
}

// connect performs the UDP tracker connection handshake.
func (c *UDPTrackerClient) connect(ctx context.Context) (uint64, error) {
	transactionID := rand.Uint32()
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, uint64(0x41727101980)) // Protocol ID
	binary.Write(buf, binary.BigEndian, uint32(actionConnect))
	binary.Write(buf, binary.BigEndian, transactionID)

	deadline, _ := ctx.Deadline()
	c.conn.SetWriteDeadline(deadline)
	if _, err := c.conn.Write(buf.Bytes()); err != nil {
		return 0, err
	}

	c.conn.SetReadDeadline(deadline)
	resp := make([]byte, 16)
	n, err := c.conn.Read(resp)
	if err != nil {
		return 0, err
	}
	if n < 16 {
		return 0, fmt.Errorf("invalid connect response size: %d", n)
	}

	respBuf := bytes.NewReader(resp)
	var action, respTransID uint32
	var connectionID uint64

	binary.Read(respBuf, binary.BigEndian, &action)
	binary.Read(respBuf, binary.BigEndian, &respTransID)
	binary.Read(respBuf, binary.BigEndian, &connectionID)

	if action != actionConnect || respTransID != transactionID {
		return 0, fmt.Errorf("invalid connect response from tracker")
	}

	return connectionID, nil
}

// announce sends the actual announce request.
func (c *UDPTrackerClient) announce(ctx context.Context, connID uint64, req *AnnounceRequest) (*TrackerResponse, error) {
	transactionID := rand.Uint32()
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, connID)
	binary.Write(buf, binary.BigEndian, uint32(actionAnnounce))
	binary.Write(buf, binary.BigEndian, transactionID)
	buf.Write(req.InfoHash[:])
	buf.Write(req.PeerID[:])
	binary.Write(buf, binary.BigEndian, req.Downloaded)
	binary.Write(buf, binary.BigEndian, req.Left)
	binary.Write(buf, binary.BigEndian, req.Uploaded)

	var event uint32
	switch req.Event {
	case "completed":
		event = 1
	case "started":
		event = 2
	case "stopped":
		event = 3
	}
	binary.Write(buf, binary.BigEndian, event)
	binary.Write(buf, binary.BigEndian, uint32(0))     // IP address
	binary.Write(buf, binary.BigEndian, rand.Uint32()) // Key
	binary.Write(buf, binary.BigEndian, int32(-1))     // NumWant (-1 for default)
	binary.Write(buf, binary.BigEndian, req.Port)

	deadline, _ := ctx.Deadline()
	c.conn.SetWriteDeadline(deadline)
	if _, err := c.conn.Write(buf.Bytes()); err != nil {
		return nil, err
	}

	resp := make([]byte, 2048)
	c.conn.SetReadDeadline(deadline)
	n, err := c.conn.Read(resp)
	if err != nil {
		return nil, err
	}

	if n < 20 {
		return nil, fmt.Errorf("announce response too small: %d bytes", n)
	}

	respBuf := bytes.NewReader(resp[:n])
	var action, respTransID uint32
	binary.Read(respBuf, binary.BigEndian, &action)
	binary.Read(respBuf, binary.BigEndian, &respTransID)

	if respTransID != transactionID {
		return nil, fmt.Errorf("transaction ID mismatch in announce response")
	}
	if action == actionError {
		errorMsg, _ := io.ReadAll(respBuf)
		return nil, fmt.Errorf("tracker error: %s", string(errorMsg))
	}

	var interval, leechers, seeders uint32
	binary.Read(respBuf, binary.BigEndian, &interval)
	binary.Read(respBuf, binary.BigEndian, &leechers)
	binary.Read(respBuf, binary.BigEndian, &seeders)

	peerData, _ := io.ReadAll(respBuf)
	peers := parseCompactPeers(peerData)

	return &TrackerResponse{
		Interval:   int(interval),
		Peers:      peers,
		Complete:   int(seeders),
		Incomplete: int(leechers),
	}, nil
}

// Close closes the UDP connection.
func (c *UDPTrackerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// retry is a helper function to retry a function with exponential backoff.
func retry(attempts int, sleep time.Duration, f func() error) error {
	if err := f(); err != nil {
		if attempts--; attempts > 0 {
			// Add jitter to avoid thundering herd problem
			jitter := time.Duration(rand.Int63n(int64(sleep)))
			sleep = sleep + jitter/2

			time.Sleep(sleep)
			return retry(attempts, 2*sleep, f)
		}
		return err
	}
	return nil
}
