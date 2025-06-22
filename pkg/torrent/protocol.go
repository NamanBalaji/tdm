package torrent

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Protocol constants
const (
	ProtocolIdentifier = "BitTorrent protocol"
	HandshakeLength    = 68
	BlockSize          = 16384 // 16KB standard block size
)

// Message types
const (
	MsgChoke         = 0
	MsgUnchoke       = 1
	MsgInterested    = 2
	MsgNotInterested = 3
	MsgHave          = 4
	MsgBitfield      = 5
	MsgRequest       = 6
	MsgPiece         = 7
	MsgCancel        = 8
	MsgPort          = 9
	MsgExtended      = 20
)

// Handshake represents the BitTorrent handshake.
type Handshake struct {
	Pstr     string
	InfoHash [20]byte
	PeerID   [20]byte
	Reserved [8]byte
}

// NewHandshake creates a new handshake message.
func NewHandshake(infoHash, peerID [20]byte) *Handshake {
	return &Handshake{
		Pstr:     ProtocolIdentifier,
		InfoHash: infoHash,
		PeerID:   peerID,
		Reserved: [8]byte{0, 0, 0, 0, 0, 0, 0, 0}, // Can set extension bits here
	}
}

// Serialize writes the handshake to a writer.
func (h *Handshake) Serialize(w io.Writer) error {
	if err := binary.Write(w, binary.BigEndian, uint8(len(h.Pstr))); err != nil {
		return err
	}
	if _, err := w.Write([]byte(h.Pstr)); err != nil {
		return err
	}
	if _, err := w.Write(h.Reserved[:]); err != nil {
		return err
	}
	if _, err := w.Write(h.InfoHash[:]); err != nil {
		return err
	}
	if _, err := w.Write(h.PeerID[:]); err != nil {
		return err
	}
	return nil
}

// ReadHandshake reads a handshake from a reader.
func ReadHandshake(r io.Reader) (*Handshake, error) {
	h := &Handshake{}

	var pstrLen uint8
	if err := binary.Read(r, binary.BigEndian, &pstrLen); err != nil {
		return nil, err
	}

	pstr := make([]byte, pstrLen)
	if _, err := io.ReadFull(r, pstr); err != nil {
		return nil, err
	}
	h.Pstr = string(pstr)

	if _, err := io.ReadFull(r, h.Reserved[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, h.InfoHash[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, h.PeerID[:]); err != nil {
		return nil, err
	}

	return h, nil
}

// Message represents a BitTorrent protocol message.
type Message struct {
	Type    uint8
	Payload []byte
}

// Serialize writes the message to a writer.
func (m *Message) Serialize(w io.Writer) error {
	length := uint32(len(m.Payload) + 1)
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, m.Type); err != nil {
		return err
	}
	if len(m.Payload) > 0 {
		if _, err := w.Write(m.Payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadMessage reads a message from a reader.
func ReadMessage(r io.Reader) (*Message, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	if length == 0 {
		// Keep-alive message
		return nil, nil
	}

	if length > 1<<20 { // 1MB max message size
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}

	msg := &Message{}
	if err := binary.Read(r, binary.BigEndian, &msg.Type); err != nil {
		return nil, err
	}

	if length > 1 {
		msg.Payload = make([]byte, length-1)
		if _, err := io.ReadFull(r, msg.Payload); err != nil {
			return nil, err
		}
	}

	return msg, nil
}

// RequestMessage represents a block request.
type RequestMessage struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

// ParseRequest parses a request message payload.
func ParseRequest(payload []byte) (*RequestMessage, error) {
	if len(payload) != 12 {
		return nil, fmt.Errorf("invalid request payload length: %d", len(payload))
	}

	return &RequestMessage{
		Index:  binary.BigEndian.Uint32(payload[0:4]),
		Begin:  binary.BigEndian.Uint32(payload[4:8]),
		Length: binary.BigEndian.Uint32(payload[8:12]),
	}, nil
}

// Serialize converts the request to bytes.
func (r *RequestMessage) Serialize() []byte {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], r.Index)
	binary.BigEndian.PutUint32(payload[4:8], r.Begin)
	binary.BigEndian.PutUint32(payload[8:12], r.Length)
	return payload
}

// PieceMessage represents a piece/block message.
type PieceMessage struct {
	Index uint32
	Begin uint32
	Block []byte
}

// ParsePiece parses a piece message payload.
func ParsePiece(payload []byte) (*PieceMessage, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("invalid piece payload length: %d", len(payload))
	}

	return &PieceMessage{
		Index: binary.BigEndian.Uint32(payload[0:4]),
		Begin: binary.BigEndian.Uint32(payload[4:8]),
		Block: payload[8:],
	}, nil
}
