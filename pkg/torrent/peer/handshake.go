package peer

import (
	"bytes"
	"errors"
)

const (
	ProtocolID   = "BitTorrent protocol"
	ReservedLen  = 8
	HandshakeLen = 1 + len(ProtocolID) + ReservedLen + 20 + 20
)

var (
	protocolBytes = []byte(ProtocolID)

	ErrInvalidHandshake = errors.New("invalid handshake length")
	ErrBadProtocol      = errors.New("wrong protocol identifier")
)

type Handshake struct {
	Protocol string
	Reserved [ReservedLen]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// Marshal encodes the handshake into a 68-byte slice.
func (h Handshake) Marshal() ([]byte, error) {
	b := make([]byte, HandshakeLen)

	// Byte 0: protocol length (19)
	b[0] = byte(len(protocolBytes))

	// Bytes 1-19: protocol string "BitTorrent protocol"
	copy(b[1:20], protocolBytes)

	// Bytes 20-27: reserved bytes
	copy(b[20:28], h.Reserved[:])

	// Bytes 28-47: info hash
	copy(b[28:48], h.InfoHash[:])

	// Bytes 48-67: peer ID
	copy(b[48:68], h.PeerID[:])

	return b, nil
}

// Unmarshal decodes a 68-byte handshake into a Handshake struct.
func Unmarshal(b []byte) (Handshake, error) {
	if len(b) != HandshakeLen {
		return Handshake{}, ErrInvalidHandshake
	}

	if b[0] != 19 || !bytes.Equal(b[1:20], protocolBytes) {
		return Handshake{}, ErrBadProtocol
	}

	var h Handshake

	h.Protocol = ProtocolID
	copy(h.Reserved[:], b[20:28])
	copy(h.InfoHash[:], b[28:48])
	copy(h.PeerID[:], b[48:68])

	return h, nil
}
