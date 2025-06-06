package torrent

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// Common errors.
var (
	ErrInvalidTorrent     = errors.New("invalid torrent file")
	ErrNoPieces           = errors.New("no pieces in torrent")
	ErrInvalidPieceLength = errors.New("invalid piece length")
	ErrNoFiles            = errors.New("no files in torrent")
)

// Metadata represents a torrent file's metadata.
type Metadata struct {
	// Announce is the URL of the tracker
	Announce string `bencode:"announce"`
	// AnnounceList is a list of lists of tracker URLs (for multi-tracker torrents)
	AnnounceList [][]string `bencode:"announce-list,omitempty"`
	// Comment is an optional comment
	Comment string `bencode:"comment,omitempty"`
	// CreatedBy indicates what program created the torrent
	CreatedBy string `bencode:"created by,omitempty"`
	// CreationDate is when the torrent was created (Unix timestamp)
	CreationDate int64 `bencode:"creation date,omitempty"`
	// Info contains the main torrent information
	Info Info `bencode:"info"`
	// InfoHash is the SHA1 hash of the bencoded info dictionary
	InfoHash [20]byte `bencode:"-"`
	// InfoHashHex is the hex representation of InfoHash
	InfoHashHex string `bencode:"-"`
}

// Info represents the info dictionary of a torrent.
type Info struct {
	// PieceLength is the number of bytes in each piece
	PieceLength int64 `bencode:"piece length"`
	// Pieces is the concatenation of all 20-byte SHA1 hash values
	Pieces []byte `bencode:"pieces"`
	// Private indicates if the torrent is private (DHT disabled)
	Private int `bencode:"private,omitempty"`
	// Name is the suggested name to save the file/directory as
	Name string `bencode:"name"`
	// Single file mode fields
	Length int64 `bencode:"length,omitempty"`
	// Multi-file mode fields
	Files []File `bencode:"files,omitempty"`
}

// File represents a file in a multi-file torrent.
type File struct {
	// Length is the length of the file in bytes
	Length int64 `bencode:"length"`
	// Path is a list of strings representing the file path
	Path []string `bencode:"path"`
}

// TrackerInfo contains all tracker URLs from the torrent.
type TrackerInfo struct {
	// Primary is the main announce URL
	Primary string
	// Tiers is the announce-list organized by tier
	Tiers [][]string
}

// FileInfo represents a file in the torrent with its absolute position.
type FileInfo struct {
	Path   string
	Length int64
	Offset int64 // Offset from the beginning of all concatenated files
}

// PieceInfo represents information about a piece.
type PieceInfo struct {
	Index  int
	Offset int64
	Length int64
	Hash   [20]byte
}

// ParseFile parses a torrent file from disk.
func ParseFile(path string) (*Metadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening torrent file: %w", err)
	}
	defer file.Close()

	return Parse(file)
}

// Parse parses a torrent from a reader.
func Parse(r io.Reader) (*Metadata, error) {
	decoder := bencode.NewDecoder(r)

	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding torrent: %w", err)
	}

	infoRaw, ok := raw["info"]
	if !ok {
		return nil, fmt.Errorf("%w: missing info dictionary", ErrInvalidTorrent)
	}

	infoBytes, err := bencode.Marshal(infoRaw)
	if err != nil {
		return nil, fmt.Errorf("encoding info for hash: %w", err)
	}
	infoHash := sha1.Sum(infoBytes)

	torrentBytes, err := bencode.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-encoding torrent: %w", err)
	}

	var metadata Metadata
	if err := bencode.Unmarshal(torrentBytes, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshaling torrent: %w", err)
	}

	metadata.InfoHash = infoHash
	metadata.InfoHashHex = hex.EncodeToString(infoHash[:])

	if err := metadata.Validate(); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// Validate checks if the metadata is valid.
func (m *Metadata) Validate() error {
	if m.Info.PieceLength <= 0 {
		return ErrInvalidPieceLength
	}

	if len(m.Info.Pieces) == 0 || len(m.Info.Pieces)%20 != 0 {
		return ErrNoPieces
	}

	if m.Info.Name == "" {
		return fmt.Errorf("%w: missing name", ErrInvalidTorrent)
	}

	// Check if it's single-file or multi-file mode
	if len(m.Info.Files) == 0 && m.Info.Length == 0 {
		return ErrNoFiles
	}

	if len(m.Info.Files) > 0 && m.Info.Length > 0 {
		return fmt.Errorf("%w: both single and multi-file mode specified", ErrInvalidTorrent)
	}

	// Validate files in multi-file mode
	for i, f := range m.Info.Files {
		if f.Length < 0 {
			return fmt.Errorf("%w: negative file length for file %d", ErrInvalidTorrent, i)
		}
		if len(f.Path) == 0 {
			return fmt.Errorf("%w: empty path for file %d", ErrInvalidTorrent, i)
		}
	}

	return nil
}

// TotalSize returns the total size of all files in the torrent.
func (m *Metadata) TotalSize() int64 {
	if m.IsSingleFile() {
		return m.Info.Length
	}

	var total int64
	for _, f := range m.Info.Files {
		total += f.Length
	}
	return total
}

// IsSingleFile returns true if this is a single-file torrent.
func (m *Metadata) IsSingleFile() bool {
	return len(m.Info.Files) == 0
}

// NumPieces returns the number of pieces in the torrent.
func (m *Metadata) NumPieces() int {
	return len(m.Info.Pieces) / 20
}

// PieceHash returns the SHA1 hash for a piece at the given index.
func (m *Metadata) PieceHash(index int) ([20]byte, error) {
	if index < 0 || index >= m.NumPieces() {
		return [20]byte{}, fmt.Errorf("piece index %d out of range", index)
	}

	var hash [20]byte
	copy(hash[:], m.Info.Pieces[index*20:(index+1)*20])
	return hash, nil
}

// GetFiles returns a list of all files with their absolute positions.
func (m *Metadata) GetFiles() []FileInfo {
	var files []FileInfo
	var offset int64

	if m.IsSingleFile() {
		files = append(files, FileInfo{
			Path:   m.Info.Name,
			Length: m.Info.Length,
			Offset: 0,
		})
	} else {
		for _, f := range m.Info.Files {
			path := filepath.Join(append([]string{m.Info.Name}, f.Path...)...)
			files = append(files, FileInfo{
				Path:   path,
				Length: f.Length,
				Offset: offset,
			})
			offset += f.Length
		}
	}

	return files
}

// GetPieces returns information about all pieces.
func (m *Metadata) GetPieces() []PieceInfo {
	pieces := make([]PieceInfo, m.NumPieces())
	totalSize := m.TotalSize()

	for i := 0; i < m.NumPieces(); i++ {
		offset := int64(i) * m.Info.PieceLength
		length := m.Info.PieceLength

		// Last piece might be smaller
		if i == m.NumPieces()-1 {
			remaining := totalSize - offset
			if remaining < length {
				length = remaining
			}
		}

		hash, _ := m.PieceHash(i)
		pieces[i] = PieceInfo{
			Index:  i,
			Offset: offset,
			Length: length,
			Hash:   hash,
		}
	}

	return pieces
}

// GetTrackers returns all tracker URLs organized by priority.
func (m *Metadata) GetTrackers() TrackerInfo {
	info := TrackerInfo{
		Primary: m.Announce,
		Tiers:   m.AnnounceList,
	}

	// If no announce-list, create one with just the primary tracker
	if len(info.Tiers) == 0 && info.Primary != "" {
		info.Tiers = [][]string{{info.Primary}}
	}

	return info
}

// CreationTime returns the creation time as a time.Time.
func (m *Metadata) CreationTime() time.Time {
	if m.CreationDate == 0 {
		return time.Time{}
	}
	return time.Unix(m.CreationDate, 0)
}

// IsPrivate returns true if this is a private torrent (DHT disabled).
func (m *Metadata) IsPrivate() bool {
	return m.Info.Private == 1
}

// Save saves the metadata to a torrent file.
func (m *Metadata) Save(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating torrent file: %w", err)
	}
	defer file.Close()

	encoder := bencode.NewEncoder(file)
	return encoder.Encode(m)
}

// Following common conventions: 16KB minimum, doubling for each power of 2 file size.
func CalculatePieceSize(totalSize int64) int64 {
	const (
		minPieceSize = 16 * 1024        // 16 KB
		maxPieceSize = 16 * 1024 * 1024 // 16 MB
	)

	// Start with minimum piece size
	pieceSize := int64(minPieceSize)

	// Double piece size for every 2GB
	for totalSize > 2*1024*1024*1024 && pieceSize < maxPieceSize {
		pieceSize *= 2
		totalSize /= 2
	}

	return pieceSize
}
