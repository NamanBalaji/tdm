package peer

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	MsgChoke = iota
	MsgUnchoke
	MsgInterested
	MsgNotInterested
	MsgHave
	MsgBitfield
	MsgRequest
	MsgPiece
	MsgCancel
	MsgPort
	// MsgKeepAlive is a special message with no payload (length 0).
	MsgKeepAlive = 10
)

var (
	ErrMsgTooBig = errors.New("message larger than 1 MiB")
	ErrMsgShort  = errors.New("message body too short")
)

const MaxMsgLen = 1 << 20 // 1 MiB

// Message is a tiny view into the wire bytes.
// Payload, Block point into Reader's internal buffer and are
// only valid until the next call to ReadMsg.
type Message struct {
	Type    byte
	Payload []byte // slice into a shared buffer
	Index   uint32 // decoded for request/piece/cancel/have (0 otherwise)
	Begin   uint32 // decoded for request/piece/cancel (0 otherwise)
	Length  uint32 // decoded for request/cancel (0 otherwise)
	Block   []byte // present only for MsgPiece
	Port    uint16 // decoded for MsgPort (0 otherwise)
}

// Reader provides zero-copy streaming message decoding.
type Reader struct {
	r   io.Reader
	buf [4 + MaxMsgLen]byte // prefix + max payload
}

// NewReader creates a new message reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// ReadMsg reads and decodes the next message from the stream.
// The returned Message's Payload and Block fields point into the Reader's
// internal buffer and are only valid until the next call to ReadMsg.
func (r *Reader) ReadMsg() (Message, error) {
	// read 4-byte length prefix
	_, err := io.ReadFull(r.r, r.buf[:4])
	if err != nil {
		return Message{}, err
	}

	l := binary.BigEndian.Uint32(r.buf[:4])
	if l == 0 {
		return Message{Type: MsgKeepAlive}, nil // keep-alive
	}

	if l > MaxMsgLen {
		return Message{}, ErrMsgTooBig
	}

	// read body (type byte + payload)
	_, err = io.ReadFull(r.r, r.buf[4:4+l])
	if err != nil {
		return Message{}, err
	}

	typ := r.buf[4]
	msg := Message{
		Type:    typ,
		Payload: r.buf[5 : 4+l], // slice into shared buffer
	}

	switch typ {
	case MsgHave:
		if len(msg.Payload) != 4 {
			return Message{}, ErrMsgShort
		}

		msg.Index = binary.BigEndian.Uint32(msg.Payload)

	case MsgRequest, MsgCancel:
		if len(msg.Payload) != 12 {
			return Message{}, ErrMsgShort
		}

		msg.Index = binary.BigEndian.Uint32(msg.Payload[0:4])
		msg.Begin = binary.BigEndian.Uint32(msg.Payload[4:8])
		msg.Length = binary.BigEndian.Uint32(msg.Payload[8:12])

	case MsgPiece:
		if len(msg.Payload) < 8 {
			return Message{}, ErrMsgShort
		}

		msg.Index = binary.BigEndian.Uint32(msg.Payload[0:4])
		msg.Begin = binary.BigEndian.Uint32(msg.Payload[4:8])
		msg.Block = msg.Payload[8:]

	case MsgPort:
		if len(msg.Payload) != 2 {
			return Message{}, ErrMsgShort
		}

		msg.Port = binary.BigEndian.Uint16(msg.Payload)

	case MsgBitfield:
		// nothing to parse; payload is the raw bitfield
	}

	return msg, nil
}

// Writer provides efficient message encoding with minimal allocations.
type Writer struct {
	w   io.Writer
	buf [12]byte // scratch for fixed-length payloads
}

// NewWriter creates a new message writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteMsg writes a message with the given type and payload.
// This is a low-level method; prefer the specific Write* methods.
func (w *Writer) WriteMsg(typ byte, payload []byte) error {
	l := uint32(1 + len(payload))

	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], l)

	if _, err := w.w.Write(prefix[:]); err != nil {
		return err
	}

	if _, err := w.w.Write([]byte{typ}); err != nil {
		return err
	}

	_, err := w.w.Write(payload)

	return err
}

// WriteKeepAlive writes a keep-alive message (4 zero bytes).
func (w *Writer) WriteKeepAlive() error {
	var prefix [4]byte // already zero

	_, err := w.w.Write(prefix[:])

	return err
}

// WriteChoke writes a choke message.
func (w *Writer) WriteChoke() error {
	return w.WriteMsg(MsgChoke, nil)
}

// WriteUnchoke writes an unchoke message.
func (w *Writer) WriteUnchoke() error {
	return w.WriteMsg(MsgUnchoke, nil)
}

// WriteInterested writes an interested message.
func (w *Writer) WriteInterested() error {
	return w.WriteMsg(MsgInterested, nil)
}

// WriteNotInterested writes a not interested message.
func (w *Writer) WriteNotInterested() error {
	return w.WriteMsg(MsgNotInterested, nil)
}

// WriteHave writes a have message with the given piece index.
func (w *Writer) WriteHave(index uint32) error {
	binary.BigEndian.PutUint32(w.buf[:4], index)
	return w.WriteMsg(MsgHave, w.buf[:4])
}

// WriteRequest writes a request message for a piece block.
func (w *Writer) WriteRequest(index, begin, length uint32) error {
	binary.BigEndian.PutUint32(w.buf[0:4], index)
	binary.BigEndian.PutUint32(w.buf[4:8], begin)
	binary.BigEndian.PutUint32(w.buf[8:12], length)

	return w.WriteMsg(MsgRequest, w.buf[:12])
}

// WriteCancel writes a cancel message for a piece block.
func (w *Writer) WriteCancel(index, begin, length uint32) error {
	binary.BigEndian.PutUint32(w.buf[0:4], index)
	binary.BigEndian.PutUint32(w.buf[4:8], begin)
	binary.BigEndian.PutUint32(w.buf[8:12], length)

	return w.WriteMsg(MsgCancel, w.buf[:12])
}

// WritePiece writes a piece message with the given block data.
// This allocates a single buffer for the entire message.
func (w *Writer) WritePiece(index, begin uint32, block []byte) error {
	buf := make([]byte, 8+len(block)) // single alloc
	binary.BigEndian.PutUint32(buf[0:4], index)
	binary.BigEndian.PutUint32(buf[4:8], begin)
	copy(buf[8:], block)

	return w.WriteMsg(MsgPiece, buf)
}

// WriteBitfield writes a bitfield message with the given bitfield data.
func (w *Writer) WriteBitfield(bits []byte) error {
	return w.WriteMsg(MsgBitfield, bits)
}

// WritePort writes a port message with the given port number.
func (w *Writer) WritePort(port uint16) error {
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], port)

	return w.WriteMsg(MsgPort, p[:])
}
