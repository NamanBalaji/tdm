package peer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	DefaultDialTimeout = 5 * time.Second
	DefaultRWTimeout   = 2 * time.Minute
)

// Conn represents a goroutine-safe BitTorrent peer connection.
type Conn struct {
	netConn net.Conn
	r       *Reader
	w       *Writer
	id      [20]byte // our peer id
	remote  net.Addr
	mu      sync.Mutex // protects writes to netConn
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// Dial establishes a connection to a peer and performs the handshake.
func Dial(ctx context.Context, addr string, infoHash, peerID [20]byte) (*Conn, error) {
	dialer := &net.Dialer{Timeout: DefaultDialTimeout}

	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return newConn(ctx, netConn, infoHash, peerID)
}

// Accept accepts a connection from a listener and performs the handshake.
func Accept(ctx context.Context, l net.Listener, infoHash, peerID [20]byte) (*Conn, error) {
	netConn, err := l.Accept()
	if err != nil {
		return nil, err
	}

	return newConn(ctx, netConn, infoHash, peerID)
}

// newConn creates a new connection and performs the handshake.
func newConn(ctx context.Context, netConn net.Conn, infoHash, peerID [20]byte) (*Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	c := &Conn{
		netConn: netConn,
		r:       NewReader(netConn),
		w:       NewWriter(netConn),
		id:      peerID,
		remote:  netConn.RemoteAddr(),
		ctx:     ctx,
		cancel:  cancel,
	}

	if err := c.handshake(infoHash); err != nil {
		c.Close()
		return nil, err
	}

	c.wg.Add(1)

	go func() {
		defer c.wg.Done()

		<-c.ctx.Done()
		c.netConn.Close()
	}()

	return c, nil
}

// handshake performs the BitTorrent handshake.
func (c *Conn) handshake(infoHash [20]byte) error {
	hs := Handshake{
		Protocol: ProtocolID,
		InfoHash: infoHash,
		PeerID:   c.id,
	}

	if err := c.netConn.SetWriteDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return err
	}

	b, err := hs.Marshal()
	if err != nil {
		return err
	}

	if _, err := c.netConn.Write(b); err != nil {
		return err
	}

	if err := c.netConn.SetReadDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return err
	}

	buf := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(c.netConn, buf); err != nil {
		return err
	}

	peerHS, err := Unmarshal(buf)
	if err != nil {
		return err
	}

	if peerHS.InfoHash != infoHash {
		return errors.New("info hash mismatch")
	}

	c.netConn.SetReadDeadline(time.Time{})
	c.netConn.SetWriteDeadline(time.Time{})

	return nil
}

// ReadMsg returns the next message (blocks until ctx done or EOF).
// Sets a per-message read deadline to detect dead peers.
func (c *Conn) ReadMsg() (Message, error) {
	if err := c.netConn.SetReadDeadline(time.Now().Add(DefaultRWTimeout)); err != nil {
		return Message{}, err
	}

	msg, err := c.r.ReadMsg()

	c.netConn.SetReadDeadline(time.Time{})

	return msg, err
}

// WriteMsg sends a message (thread-safe).
// Note: This is a low-level method; prefer the specific Write* methods.
func (c *Conn) WriteMsg(typ byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteMsg(typ, payload)
}

// WriteKeepAlive writes a keep-alive message.
func (c *Conn) WriteKeepAlive() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteKeepAlive()
}

// WriteChoke writes a choke message.
func (c *Conn) WriteChoke() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteChoke()
}

// WriteUnchoke writes an unchoke message.
func (c *Conn) WriteUnchoke() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteUnchoke()
}

// WriteInterested writes an interested message.
func (c *Conn) WriteInterested() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteInterested()
}

// WriteNotInterested writes a not interested message.
func (c *Conn) WriteNotInterested() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteNotInterested()
}

// WriteHave writes a have message with the given piece index.
func (c *Conn) WriteHave(index uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteHave(index)
}

// WriteRequest writes a request message for a piece block.
func (c *Conn) WriteRequest(index, begin, length uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteRequest(index, begin, length)
}

// WriteCancel writes a cancel message for a piece block.
func (c *Conn) WriteCancel(index, begin, length uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteCancel(index, begin, length)
}

// WritePiece writes a piece message with the given block data.
func (c *Conn) WritePiece(index, begin uint32, block []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WritePiece(index, begin, block)
}

// WriteBitfield writes a bitfield message with the given bitfield data.
func (c *Conn) WriteBitfield(bits []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WriteBitfield(bits)
}

// WritePort writes a port message with the given port number.
func (c *Conn) WritePort(port uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.w.WritePort(port)
}

// RemoteAddr returns the remote address of the connection.
func (c *Conn) RemoteAddr() net.Addr {
	return c.remote
}

// PeerID returns our peer ID.
func (c *Conn) PeerID() [20]byte {
	return c.id
}

// Close shuts down the connection and waits for any goroutines to finish.
func (c *Conn) Close() error {
	c.cancel()
	err := c.netConn.Close()
	c.wg.Wait()

	return err
}
