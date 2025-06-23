package torrent_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent"
)

func TestNewBitfield(t *testing.T) {
	tests := []struct {
		name        string
		numPieces   int
		expectedLen int // in bytes
	}{
		{"Zero pieces", 0, 0},
		{"1 piece", 1, 1},
		{"7 pieces", 7, 1},
		{"8 pieces", 8, 1},
		{"9 pieces", 9, 2},
		{"16 pieces", 16, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bf := torrent.NewBitfield(tt.numPieces)
			if len(bf.Bytes()) != tt.expectedLen {
				t.Errorf("expected byte slice length %d, got %d", tt.expectedLen, len(bf.Bytes()))
			}
		})
	}
}

func TestBitfield_HasSetPiece(t *testing.T) {
	numPieces := 17
	bf := torrent.NewBitfield(numPieces)

	tests := []struct {
		name      string
		index     int
		shouldErr bool
	}{
		{"Set piece 0", 0, false},
		{"Set piece 8", 8, false},
		{"Set piece 16 (last)", 16, false},
		{"Set piece 5", 5, false},
		{"Index out of range (negative)", -1, true},
		{"Index out of range (too large)", numPieces, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initially, should not have the piece
			if !tt.shouldErr && bf.HasPiece(tt.index) {
				t.Errorf("initially has piece %d, should not", tt.index)
			}

			err := bf.SetPiece(tt.index)
			hasErr := err != nil
			if hasErr != tt.shouldErr {
				t.Errorf("SetPiece() error = %v, wantErr %v", err, tt.shouldErr)
			}

			if !tt.shouldErr {
				if !bf.HasPiece(tt.index) {
					t.Errorf("expected to have piece %d after setting, but did not", tt.index)
				}
			}
		})
	}

	// Verify other pieces are unaffected
	if bf.HasPiece(1) {
		t.Error("piece 1 should not be set, but it is")
	}
	if bf.HasPiece(15) {
		t.Error("piece 15 should not be set, but it is")
	}
}

func TestBitfield_CountAndCompletion(t *testing.T) {
	bf := torrent.NewBitfield(10)

	if bf.Count() != 0 {
		t.Errorf("expected initial count 0, got %d", bf.Count())
	}
	if bf.IsComplete() {
		t.Error("expected bitfield to not be complete initially")
	}

	bf.SetPiece(1)
	bf.SetPiece(3)
	bf.SetPiece(5)

	if bf.Count() != 3 {
		t.Errorf("expected count 3, got %d", bf.Count())
	}

	// Set all pieces
	for i := 0; i < 10; i++ {
		bf.SetPiece(i)
	}

	if bf.Count() != 10 {
		t.Errorf("expected count 10 after setting all, got %d", bf.Count())
	}
	if !bf.IsComplete() {
		t.Error("expected bitfield to be complete after setting all pieces")
	}
}

func TestNewBitfieldFromBytes(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		numPieces   int
		shouldErr   bool
		checkPieces []int // pieces that should be set
	}{
		{"Valid", []byte{0b10100000}, 8, false, []int{0, 2}},
		{"Valid multi-byte", []byte{0b10000001, 0b01000000}, 16, false, []int{0, 7, 9}},
		{"Valid non-full last byte", []byte{0b10000001, 0b01000000}, 10, false, []int{0, 7, 9}},
		{"Invalid length (too short)", []byte{0b10100000}, 9, true, nil},
		{"Invalid length (too long)", []byte{0b10100000, 0b00000000}, 8, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bf, err := torrent.NewBitfieldFromBytes(tt.data, tt.numPieces)
			hasErr := err != nil
			if hasErr != tt.shouldErr {
				t.Fatalf("NewBitfieldFromBytes() error = %v, wantErr %v", err, tt.shouldErr)
			}

			if !tt.shouldErr {
				if !bytes.Equal(bf.Bytes(), tt.data) {
					t.Errorf("Bytes() mismatch: got %v, want %v", bf.Bytes(), tt.data)
				}
				for i := 0; i < tt.numPieces; i++ {
					has := bf.HasPiece(i)
					shouldHave := false
					for _, p := range tt.checkPieces {
						if i == p {
							shouldHave = true
							break
						}
					}
					if has != shouldHave {
						t.Errorf("piece %d: got has=%v, want has=%v", i, has, shouldHave)
					}
				}
			}
		})
	}
}

func TestBitfield_Concurrency(t *testing.T) {
	numPieces := 100
	bf := torrent.NewBitfield(numPieces)
	var wg sync.WaitGroup

	// Run concurrent writers
	for i := 0; i < numPieces; i++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			bf.SetPiece(p)
		}(i)
	}

	// Run concurrent readers
	for i := 0; i < numPieces*10; i++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			bf.HasPiece(p)
		}(i % numPieces)
	}

	wg.Wait()

	if !bf.IsComplete() {
		t.Errorf("expected bitfield to be complete after concurrent sets, count is %d", bf.Count())
	}
}
