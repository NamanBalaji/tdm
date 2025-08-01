package peer_test

import (
	"errors"
	"fmt"
	"github.com/NamanBalaji/tdm/pkg/torrent/peer"
	"testing"
)

func TestHandshake_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		input peer.Handshake
	}{
		{
			name: "basic handshake",
			input: peer.Handshake{
				Protocol: peer.ProtocolID,
				Reserved: [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x01},
				InfoHash: [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
				PeerID:   [20]byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T'},
			},
		},
		{
			name: "zero values",
			input: peer.Handshake{
				Protocol: peer.ProtocolID,
				Reserved: [8]byte{},
				InfoHash: [20]byte{},
				PeerID:   [20]byte{},
			},
		},
		{
			name: "max values",
			input: peer.Handshake{
				Protocol: peer.ProtocolID,
				Reserved: [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
				InfoHash: [20]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
				PeerID:   [20]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			},
		},
		{
			name: "extension support",
			input: peer.Handshake{
				Protocol: peer.ProtocolID,
				Reserved: [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x01},
				InfoHash: [20]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x11, 0x22, 0x33, 0x44},
				PeerID:   [20]byte{'-', 'T', 'D', 'M', '0', '1', '0', '0', '-', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0', '1'},
			},
		},
		{
			name: "different protocol in struct (should still use constant)",
			input: peer.Handshake{
				Protocol: "Wrong Protocol",
				Reserved: [8]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0},
				InfoHash: [20]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67},
				PeerID:   [20]byte{'T', 'E', 'S', 'T', '-', 'P', 'E', 'E', 'R', '-', 'I', 'D', '-', 'H', 'E', 'R', 'E', '!', '!', '!'},
			},
		},
		{
			name: "common client peer ID format",
			input: peer.Handshake{
				Protocol: peer.ProtocolID,
				Reserved: [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x05},
				InfoHash: [20]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD},
				PeerID:   [20]byte{'-', 'q', 'B', '4', '4', '9', '0', '-', 'r', 'a', 'n', 'd', 'o', 'm', 'p', 'e', 'e', 'r', 'i', 'd'}, // qBittorrent-style
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.input.Marshal()
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			if len(data) != peer.HandshakeLen {
				t.Errorf("Marshal() length = %d, want %d", len(data), peer.HandshakeLen)
			}

			got, err := peer.Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			if got.Protocol != peer.ProtocolID {
				t.Errorf("Protocol = %q, want %q", got.Protocol, peer.ProtocolID)
			}
			if got.Reserved != tt.input.Reserved {
				t.Errorf("Reserved = %v, want %v", got.Reserved, tt.input.Reserved)
			}
			if got.InfoHash != tt.input.InfoHash {
				t.Errorf("InfoHash = %v, want %v", got.InfoHash, tt.input.InfoHash)
			}
			if got.PeerID != tt.input.PeerID {
				t.Errorf("PeerID = %v, want %v", got.PeerID, tt.input.PeerID)
			}
		})
	}
}

func TestUnmarshalHandshake_InvalidLength(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{
			name:    "too short by 1 byte",
			input:   make([]byte, 67),
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "too long by 1 byte",
			input:   make([]byte, 69),
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "empty slice",
			input:   []byte{},
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "way too short",
			input:   make([]byte, 10),
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "way too long",
			input:   make([]byte, 200),
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "single byte",
			input:   []byte{19},
			wantErr: peer.ErrInvalidHandshake,
		},
		{
			name:    "exactly 19 bytes (just protocol)",
			input:   append([]byte{19}, []byte(peer.ProtocolID)...),
			wantErr: peer.ErrInvalidHandshake,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := peer.Unmarshal(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Unmarshal() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestUnmarshalHandshake_BadProtocol(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func() []byte
		wantErr error
	}{
		{
			name: "protocol length too small",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[0] = 18
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "protocol length too large",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[0] = 20
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "zero protocol length",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[0] = 0
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "max protocol length",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[0] = 255
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "wrong protocol string - first char",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[1] = 'X'
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "wrong protocol string - last char",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[19] = 'X'
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "wrong protocol string - middle char",
			setupFn: func() []byte {
				h := peer.Handshake{Protocol: peer.ProtocolID}
				data, _ := h.Marshal()
				data[10] = 'X'
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "completely wrong protocol",
			setupFn: func() []byte {
				data := make([]byte, 68)
				data[0] = 19
				copy(data[1:20], "HTTP/1.1 Protocol  ")
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "case sensitive protocol",
			setupFn: func() []byte {
				data := make([]byte, 68)
				data[0] = 19
				copy(data[1:20], "bittorrent protocol")
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "protocol with wrong content but right length",
			setupFn: func() []byte {
				data := make([]byte, 68)
				data[0] = 19
				copy(data[1:20], "BitTorrent protocoX")
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.setupFn()
			_, err := peer.Unmarshal(data)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Unmarshal() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandshake_MarshalFormat(t *testing.T) {
	h := peer.Handshake{
		Protocol: peer.ProtocolID,
		Reserved: [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		InfoHash: [20]byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20, 0x21, 0x22, 0x23},
		PeerID:   [20]byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F, 0x40, 0x41, 0x42, 0x43},
	}

	data, err := h.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	expected := make([]byte, 68)
	expected[0] = 19
	copy(expected[1:20], []byte(peer.ProtocolID))
	copy(expected[20:28], h.Reserved[:])
	copy(expected[28:48], h.InfoHash[:])
	copy(expected[48:68], h.PeerID[:])

	if len(data) != len(expected) {
		t.Errorf("Length mismatch: got %d, want %d", len(data), len(expected))
	}

	for i, b := range expected {
		if i < len(data) && data[i] != b {
			t.Errorf("Byte %d: got 0x%02X, want 0x%02X", i, data[i], b)
		}
	}

	if data[0] != 19 {
		t.Errorf("Protocol length at byte 0: got %d, want 19", data[0])
	}

	protocolSection := data[1:20]
	if string(protocolSection) != peer.ProtocolID {
		t.Errorf("Protocol section: got %q, want %q", string(protocolSection), peer.ProtocolID)
	}

	if data[20] != 0x01 || data[27] != 0x08 {
		t.Errorf("Reserved section boundaries incorrect")
	}

	if data[28] != 0x10 || data[47] != 0x23 {
		t.Errorf("InfoHash section boundaries incorrect")
	}

	if data[48] != 0x30 || data[67] != 0x43 {
		t.Errorf("PeerID section boundaries incorrect")
	}
}

func TestUnmarshalHandshake_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		setupFn func() []byte
		wantErr error
	}{
		{
			name: "valid handshake with nulls",
			setupFn: func() []byte {
				data := make([]byte, 68)
				data[0] = 19
				copy(data[1:20], peer.ProtocolID)
				// rest stays zero
				return data
			},
			wantErr: nil,
		},
		{
			name: "boundary test - protocol length edge",
			setupFn: func() []byte {
				data := make([]byte, 68)
				data[0] = 1
				return data
			},
			wantErr: peer.ErrBadProtocol,
		},
		{
			name: "all bytes different in each section",
			setupFn: func() []byte {
				h := peer.Handshake{
					Protocol: peer.ProtocolID,
					Reserved: [8]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11},
					InfoHash: [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13, 0x14},
					PeerID:   [20]byte{0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE, 0xFF, 0x00, 0x01, 0x02, 0x03},
				}
				data, _ := h.Marshal()
				return data
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.setupFn()
			result, err := peer.Unmarshal(data)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Unmarshal() error = %v, want %v", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("Unmarshal() unexpected error = %v", err)
				}
				if result.Protocol != peer.ProtocolID {
					t.Errorf("Protocol = %q, want %q", result.Protocol, peer.ProtocolID)
				}
			}
		})
	}
}

func TestMarshal_AlwaysReturns68Bytes(t *testing.T) {
	testCases := []peer.Handshake{
		{},
		{Protocol: "wrong"},
		{Protocol: peer.ProtocolID},
		{
			Protocol: "",
			Reserved: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			InfoHash: [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			PeerID:   [20]byte{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
		},
	}

	for i, h := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			data, err := h.Marshal()
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if len(data) != 68 {
				t.Errorf("Marshal() returned %d bytes, want 68", len(data))
			}
			if data[0] != 19 {
				t.Errorf("First byte = %d, want 19", data[0])
			}
		})
	}
}

func TestHandshake_MarshalErrorCases(t *testing.T) {
	testCases := []peer.Handshake{
		{},
		{Protocol: "anything"},
		{
			Protocol: peer.ProtocolID,
			Reserved: [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			InfoHash: [20]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			PeerID:   [20]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		},
	}

	for i, h := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			data, err := h.Marshal()

			if err != nil {
				t.Errorf("Marshal() returned unexpected error: %v", err)
			}

			if len(data) != 68 {
				t.Errorf("Marshal() returned %d bytes, want 68", len(data))
			}

			if len(data) > 0 && data[0] != 19 {
				t.Errorf("First byte = %d, want 19", data[0])
			}
		})
	}
}
