package torrent

import (
	"math/rand"
	"sort"
	"sync"
	"time"
)

// PiecePickerStrategy defines piece selection strategies.
type PiecePickerStrategy int

const (
	PickerRandom PiecePickerStrategy = iota
	PickerRarest
	PickerSequential
	PickerEndGame
)

// PiecePicker selects which pieces to download next.
type PiecePicker struct {
	pieceManager *PieceManager
	availability map[int]int // piece index -> number of peers that have it
	inProgress   map[int]bool
	strategy     PiecePickerStrategy
	mu           sync.RWMutex
	rand         *rand.Rand
}

// NewPiecePicker creates a new piece picker.
func NewPiecePicker(pm *PieceManager, strategy PiecePickerStrategy) *PiecePicker {
	return &PiecePicker{
		pieceManager: pm,
		availability: make(map[int]int),
		inProgress:   make(map[int]bool),
		strategy:     strategy,
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// UpdateAvailability updates piece availability from a peer's bitfield.
func (pp *PiecePicker) UpdateAvailability(peerBitfield *Bitfield) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	for i := range pp.pieceManager.totalPieces {
		if peerBitfield.HasPiece(i) {
			pp.availability[i]++
		}
	}
}

// PickPiece selects the next piece to download.
func (pp *PiecePicker) PickPiece(peerBitfield *Bitfield) (int, bool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	candidates := pp.getCandidates(peerBitfield)
	if len(candidates) == 0 {
		return -1, false
	}

	var selected int
	switch pp.strategy {
	case PickerRandom:
		selected = candidates[pp.rand.Intn(len(candidates))]
	case PickerRarest:
		selected = pp.pickRarest(candidates)
	case PickerSequential:
		selected = pp.pickSequential(candidates)
	default:
		// Fallback to random if strategy is not set or invalid
		selected = candidates[pp.rand.Intn(len(candidates))]
	}

	pp.inProgress[selected] = true
	return selected, true
}

// getCandidates returns pieces that can be downloaded.
func (pp *PiecePicker) getCandidates(peerBitfield *Bitfield) []int {
	var candidates []int

	for i := range pp.pieceManager.totalPieces {
		// Skip if we already have it
		if pp.pieceManager.verified.HasPiece(i) {
			continue
		}

		// Skip if already downloading
		if pp.inProgress[i] {
			continue
		}

		// Skip if peer doesn't have it
		if !peerBitfield.HasPiece(i) {
			continue
		}

		candidates = append(candidates, i)
	}

	return candidates
}

// pickRarest selects the rarest piece among candidates.
func (pp *PiecePicker) pickRarest(candidates []int) int {
	if len(candidates) == 0 {
		return -1
	}

	// Sort by rarity (least available first)
	sort.Slice(candidates, func(i, j int) bool {
		availI := pp.availability[candidates[i]]
		availJ := pp.availability[candidates[j]]
		if availI == availJ {
			// If rarity is the same, prefer lower index
			return candidates[i] < candidates[j]
		}
		return availI < availJ
	})

	return candidates[0]
}

// pickSequential selects pieces in order.
func (pp *PiecePicker) pickSequential(candidates []int) int {
	if len(candidates) == 0 {
		return -1
	}

	sort.Ints(candidates)
	return candidates[0]
}

// MarkComplete marks a piece as no longer in progress.
func (pp *PiecePicker) MarkComplete(index int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	delete(pp.inProgress, index)
}

// MarkFailed marks a piece as failed and available for retry.
func (pp *PiecePicker) MarkFailed(index int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	delete(pp.inProgress, index)
}

// InProgressCount returns the number of pieces currently being downloaded.
func (pp *PiecePicker) InProgressCount() int {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return len(pp.inProgress)
}
