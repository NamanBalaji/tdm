package torrent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// Protocol constants.
const (
	ProtocolIdentifier = "BitTorrent protocol"
	HandshakeLength    = 68
	BlockSize          = 16384 // 16KB standard block size
)

// Message types.
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

// Extended message IDs.
const (
	ExtendedHandshakeID = 0
)

// Handshake represents the BitTorrent handshake.
type Handshake struct {
	Pstr     string
	InfoHash [20]byte
	PeerID   [20]byte
	Reserved [8]byte
}

// NewHandshake creates a new handshake message with extensions enabled.
func NewHandshake(infoHash, peerID [20]byte) *Handshake {
	h := &Handshake{
		Pstr:     ProtocolIdentifier,
		InfoHash: infoHash,
		PeerID:   peerID,
	}
	// Set the 20th bit of the reserved bytes to indicate support for extensions.
	h.Reserved[5] |= 0x10

	return h
}

// HasExtensionSupport checks if the peer supports the extension protocol.
func (h *Handshake) HasExtensionSupport() bool {
	return (h.Reserved[5] & 0x10) != 0
}

// Serialize writes the handshake to a writer.
func (h *Handshake) Serialize(w io.Writer) error {
	err := binary.Write(w, binary.BigEndian, uint8(len(h.Pstr)))
	if err != nil {
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
	err := binary.Read(r, binary.BigEndian, &pstrLen)
	if err != nil {
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
	err := binary.Write(w, binary.BigEndian, length)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.BigEndian, m.Type)

	if err != nil {
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
	err := binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}

	if length == 0 {
		// Keep-alive message
		return nil, nil
	}

	if length > 2*BlockSize { // Safeguard against massive messages
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return &Message{
		Type:    payload[0],
		Payload: payload[1:],
	}, nil
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

// ExtendedHandshakePayload represents the payload of an extended handshake message.
type ExtendedHandshakePayload struct {
	M map[string]int
	P uint16 // Listening port
	V string // Client version
}

// ParseExtendedHandshake manually parses the bencoded data into the struct.
func ParseExtendedHandshake(data []byte) (*ExtendedHandshakePayload, error) {
	decoded, _, err := bencode.Decode(data)
	if err != nil {
		return nil, err
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("extended handshake payload is not a dictionary")
	}

	payload := &ExtendedHandshakePayload{
		M: make(map[string]int),
	}

	if mVal, ok := dict["m"]; ok {
		if mDict, ok := mVal.(map[string]any); ok {
			for k, v := range mDict {
				if vInt, ok := v.(int64); ok {
					payload.M[k] = int(vInt)
				}
			}
		}
	}

	if pVal, ok := dict["p"]; ok {
		if pInt, ok := pVal.(int64); ok {
			payload.P = uint16(pInt)
		}
	}

	if vVal, ok := dict["v"]; ok {
		if vBytes, ok := vVal.([]byte); ok {
			payload.V = string(vBytes)
		}
	}

	return payload, nil
}
