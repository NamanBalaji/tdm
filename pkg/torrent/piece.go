package torrent

import (
	"crypto/sha1"
	"fmt"
	"sync"
)

// PieceState represents the download state of a piece.
type PieceState int

const (
	PieceStateEmpty PieceState = iota
	PieceStateDownloading
	PieceStateComplete
	PieceStateVerified
)

// Block represents a block within a piece.
type Block struct {
	PieceIndex int
	Index      int
	Offset     int
	Length     int
	Data       []byte
}

// Piece represents a piece in the torrent.
type Piece struct {
	Index    int
	Length   int64
	Hash     [20]byte
	State    PieceState
	Blocks   []*Block
	mu       sync.RWMutex
	received int // Number of blocks received
}

// NewPiece creates a new piece.
func NewPiece(index int, length int64, hash [20]byte) *Piece {
	numBlocks := int((length + BlockSize - 1) / BlockSize)
	blocks := make([]*Block, numBlocks)

	for i := range numBlocks {
		blockOffset := i * BlockSize

		blockLength := BlockSize
		if i == numBlocks-1 {
			// Last block might be smaller
			blockLength = int(length) - blockOffset
		}

		blocks[i] = &Block{
			PieceIndex: index,
			Index:      i,
			Offset:     blockOffset,
			Length:     blockLength,
		}
	}

	return &Piece{
		Index:  index,
		Length: length,
		Hash:   hash,
		State:  PieceStateEmpty,
		Blocks: blocks,
	}
}

// AddBlock adds a downloaded block to the piece.
func (p *Piece) AddBlock(offset int, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	blockIndex := offset / BlockSize
	if blockIndex >= len(p.Blocks) {
		return fmt.Errorf("invalid block index %d for piece %d", blockIndex, p.Index)
	}

	block := p.Blocks[blockIndex]
	if block.Data != nil {
		// Block already received
		return nil
	}

	if len(data) != block.Length {
		return fmt.Errorf("block length mismatch: expected %d, got %d", block.Length, len(data))
	}

	block.Data = make([]byte, len(data))
	copy(block.Data, data)

	p.received++

	if p.received == len(p.Blocks) {
		p.State = PieceStateComplete
	}

	return nil
}

// IsComplete returns true if all blocks have been received.
func (p *Piece) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.State == PieceStateComplete || p.State == PieceStateVerified
}

// Verify checks if the piece data matches its hash.
func (p *Piece) Verify() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.State != PieceStateComplete {
		return false
	}

	// Concatenate all blocks
	data := make([]byte, 0, p.Length)
	for _, block := range p.Blocks {
		if block.Data == nil {
			return false
		}

		data = append(data, block.Data...)
	}

	// Calculate hash
	hash := sha1.Sum(data)
	if hash == p.Hash {
		p.State = PieceStateVerified
		return true
	}

	return false
}

// ReadBlock reads a block of data from a verified piece.
func (p *Piece) ReadBlock(offset, length int) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.State != PieceStateVerified {
		return nil, fmt.Errorf("cannot read from unverified piece %d", p.Index)
	}

	if offset < 0 || offset+length > int(p.Length) {
		return nil, fmt.Errorf("read request for piece %d exceeds bounds (offset: %d, len: %d, pieceLen: %d)", p.Index, offset, length, p.Length)
	}

	blockIndex := offset / BlockSize
	if blockIndex >= len(p.Blocks) {
		return nil, fmt.Errorf("invalid block for offset %d in piece %d", offset, p.Index)
	}

	block := p.Blocks[blockIndex]
	if block.Offset != offset || block.Length != length {
		return nil, fmt.Errorf("request for piece %d does not align with block structure", p.Index)
	}

	return block.Data, nil
}

// GetMissingBlocks returns blocks that haven't been downloaded yet for this piece.
func (p *Piece) GetMissingBlocks() []*Block {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var missing []*Block

	for _, block := range p.Blocks {
		if block.Data == nil {
			missing = append(missing, block)
		}
	}

	return missing
}

// Reset clears the piece data and resets state.
func (p *Piece) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.State = PieceStateEmpty

	p.received = 0
	for _, block := range p.Blocks {
		block.Data = nil
	}
}

// PieceManager manages all pieces in a torrent.
type PieceManager struct {
	pieces       []*Piece
	totalPieces  int
	completed    *Bitfield
	verified     *Bitfield
	mu           sync.RWMutex
	pieceLength  int64
	lastPieceLen int64
}

// NewPieceManager creates a new piece manager.
func NewPieceManager(metainfo *Metainfo) *PieceManager {
	totalPieces := metainfo.PieceCount()
	pieces := make([]*Piece, totalPieces)
	hashes := metainfo.GetPieceHashes()

	totalSize := metainfo.TotalSize()
	pieceLength := metainfo.Info.PieceLength
	lastPieceLen := totalSize - (int64(totalPieces-1) * pieceLength)

	for i := range totalPieces {
		length := pieceLength
		if i == totalPieces-1 {
			length = lastPieceLen
		}

		pieces[i] = NewPiece(i, length, hashes[i])
	}

	return &PieceManager{
		pieces:       pieces,
		totalPieces:  totalPieces,
		completed:    NewBitfield(totalPieces),
		verified:     NewBitfield(totalPieces),
		pieceLength:  pieceLength,
		lastPieceLen: lastPieceLen,
	}
}

// GetPiece returns a piece by index.
func (pm *PieceManager) GetPiece(index int) (*Piece, error) {
	if index < 0 || index >= pm.totalPieces {
		return nil, fmt.Errorf("piece index %d out of range", index)
	}

	return pm.pieces[index], nil
}

// GetAllMissingBlocks returns all blocks that have not yet been downloaded from any piece.
// This is primarily used to enter and operate in endgame mode.
func (pm *PieceManager) GetAllMissingBlocks() []*Block {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var missing []*Block

	for _, piece := range pm.pieces {
		if !pm.verified.HasPiece(piece.Index) {
			missing = append(missing, piece.GetMissingBlocks()...)
		}
	}

	return missing
}

// GetPieceLength returns the length of a specific piece.
func (pm *PieceManager) GetPieceLength(index int) int64 {
	if index == pm.totalPieces-1 {
		return pm.lastPieceLen
	}

	return pm.pieceLength
}

// IsComplete checks if all pieces have been downloaded and verified.
func (pm *PieceManager) IsComplete() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.verified.IsComplete()
}

// MarkVerified marks a piece as verified.
func (pm *PieceManager) MarkVerified(index int) error {
	return pm.verified.SetPiece(index)
}

// Progress returns download progress as a percentage.
func (pm *PieceManager) Progress() float64 {
	verified := float64(pm.verified.Count())

	total := float64(pm.totalPieces)
	if total == 0 {
		return 0
	}

	return (verified / total) * 100
}
