package peer

// This file defines the BitTorrent handshake structure and
// serialization functions. The handshake is a fixed-length
// 68‑byte message exchanged between peers at connection setup. The
// implementation is identical to the provided handshake.go with
// contextual comments and is placed under the peer package.

import (
	"bytes"
	"errors"
)

const (
	// ProtocolID defines the standard BitTorrent protocol identifier.
	ProtocolID = "BitTorrent protocol"
	// ReservedLen is the number of reserved bytes in the handshake.
	ReservedLen = 8
	// HandshakeLen is the fixed length of a handshake message.
	HandshakeLen = 1 + len(ProtocolID) + ReservedLen + 20 + 20
)

var (
	protocolBytes = []byte(ProtocolID)
	// ErrInvalidHandshake indicates a handshake message of incorrect length.
	ErrInvalidHandshake = errors.New("invalid handshake length")
	// ErrBadProtocol indicates a mismatch in the protocol identifier.
	ErrBadProtocol = errors.New("wrong protocol identifier")
)

// Handshake represents the structure of the BitTorrent handshake.
// See BEP 3 for details.
type Handshake struct {
	Protocol string
	Reserved [ReservedLen]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// Marshal encodes the handshake into a 68‑byte slice. The
// serialized representation adheres to the standard layout.
func (h Handshake) Marshal() ([]byte, error) {
	b := make([]byte, HandshakeLen)
	// Byte 0: protocol length (19)
	b[0] = byte(len(protocolBytes))
	// Bytes 1‑19: protocol string "BitTorrent protocol"
	copy(b[1:20], protocolBytes)
	// Bytes 20‑27: reserved bytes
	copy(b[20:28], h.Reserved[:])
	// Bytes 28‑47: info hash
	copy(b[28:48], h.InfoHash[:])
	// Bytes 48‑67: peer ID
	copy(b[48:68], h.PeerID[:])

	return b, nil
}

// Unmarshal decodes a 68‑byte handshake into a Handshake struct.
func Unmarshal(b []byte) (Handshake, error) {
	if len(b) != HandshakeLen {
		return Handshake{}, ErrInvalidHandshake
	}
	// Verify the protocol identifier.
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
