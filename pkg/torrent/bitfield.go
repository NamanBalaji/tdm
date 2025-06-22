package torrent

import (
	"fmt"
	"sync"
)

// Bitfield represents which pieces a peer has.
type Bitfield struct {
	bits []byte
	len  int
	mu   sync.RWMutex
}

// NewBitfield creates a new bitfield of the given length.
func NewBitfield(numPieces int) *Bitfield {
	numBytes := (numPieces + 7) / 8
	return &Bitfield{
		bits: make([]byte, numBytes),
		len:  numPieces,
	}
}

// NewBitfieldFromBytes creates a bitfield from raw bytes.
func NewBitfieldFromBytes(data []byte, numPieces int) (*Bitfield, error) {
	expectedBytes := (numPieces + 7) / 8
	if len(data) != expectedBytes {
		return nil, fmt.Errorf("invalid bitfield length: got %d bytes, expected %d", len(data), expectedBytes)
	}

	bf := &Bitfield{
		bits: make([]byte, len(data)),
		len:  numPieces,
	}
	copy(bf.bits, data)
	return bf, nil
}

// SetPiece marks a piece as available.
func (bf *Bitfield) SetPiece(index int) error {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	if index < 0 || index >= bf.len {
		return fmt.Errorf("piece index %d out of range [0, %d)", index, bf.len)
	}

	byteIndex := index / 8
	bitIndex := uint(index % 8)
	bf.bits[byteIndex] |= 1 << (7 - bitIndex)
	return nil
}

// HasPiece checks if a piece is available.
func (bf *Bitfield) HasPiece(index int) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	if index < 0 || index >= bf.len {
		return false
	}

	byteIndex := index / 8
	bitIndex := uint(index % 8)
	return bf.bits[byteIndex]&(1<<(7-bitIndex)) != 0
}

// Bytes returns the raw bitfield bytes.
func (bf *Bitfield) Bytes() []byte {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	result := make([]byte, len(bf.bits))
	copy(result, bf.bits)
	return result
}

// Count returns the number of pieces marked as available.
func (bf *Bitfield) Count() int {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	count := 0
	for i := 0; i < bf.len; i++ {
		if bf.HasPiece(i) {
			count++
		}
	}
	return count
}

// IsComplete returns true if all pieces are available.
func (bf *Bitfield) IsComplete() bool {
	return bf.Count() == bf.len
}
