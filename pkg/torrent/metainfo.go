package torrent

import (
	"crypto/sha1"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

type Metainfo struct {
	// Core torrent metadata
	Announce     string
	AnnounceList [][]string
	Comment      string
	CreatedBy    string
	CreationDate int64
	Encoding     string
	Info         Info

	// The raw bencoded bytes of the Info dictionary. It's populated once during parsing.
	infoBytes []byte

	// Cached values for performance, calculated on first access.
	infoHash        [20]byte
	infoHashOnce    sync.Once
	pieceHashes     [][20]byte
	pieceHashesOnce sync.Once
}

// Info represents the info dictionary from a .torrent file.
type Info struct {
	Name        string
	PieceLength int64
	Pieces      string

	// Single-file mode
	Length int64
	MD5Sum string

	// Multi-file mode
	Files []File
}

// File represents a file in a multi-file torrent.
type File struct {
	Length int64
	Path   []string
	MD5Sum string
}

// InfoHash returns the SHA-1 hash of the bencoded info dictionary.
func (m *Metainfo) InfoHash() [20]byte {
	m.infoHashOnce.Do(func() {
		m.infoHash = sha1.Sum(m.infoBytes)
	})
	return m.infoHash
}

// TotalSize returns the total size of all files in the torrent.
func (m *Metainfo) TotalSize() int64 {
	if m.Info.Length > 0 {
		return m.Info.Length
	}

	var total int64
	for _, file := range m.Info.Files {
		total += file.Length
	}
	return total
}

// PieceCount returns the number of pieces in this torrent.
func (m *Metainfo) PieceCount() int {
	return len(m.Info.Pieces) / 20
}

// GetPieceHashes returns all piece hashes as a slice of byte arrays.
func (m *Metainfo) GetPieceHashes() [][20]byte {
	m.pieceHashesOnce.Do(func() {
		pieceCount := m.PieceCount()
		if pieceCount == 0 {
			return
		}
		hashes := make([][20]byte, pieceCount)
		for i := range pieceCount {
			start := i * 20
			copy(hashes[i][:], m.Info.Pieces[start:start+20])
		}
		m.pieceHashes = hashes
	})
	return m.pieceHashes
}

// GetPieceHash returns the hash for a specific piece index.
func (m *Metainfo) GetPieceHash(index int) ([20]byte, bool) {
	hashes := m.GetPieceHashes() // This will use the cached version
	if index < 0 || index >= len(hashes) {
		var hash [20]byte
		return hash, false
	}
	return hashes[index], true
}

// GetAnnounceURLs returns all announce URLs (primary + announce-list).
func (m *Metainfo) GetAnnounceURLs() []string {
	urls := make(map[string]struct{})
	if m.Announce != "" {
		urls[m.Announce] = struct{}{}
	}

	for _, tier := range m.AnnounceList {
		for _, u := range tier {
			urls[u] = struct{}{}
		}
	}

	uniqueURLs := make([]string, 0, len(urls))
	for u := range urls {
		uniqueURLs = append(uniqueURLs, u)
	}
	return uniqueURLs
}

// GetLastPieceLength returns the length of the last piece.
func (m *Metainfo) getLastPieceLength() int64 {
	totalSize := m.TotalSize()
	pieceLength := m.Info.PieceLength

	if totalSize == 0 {
		return 0
	}
	if totalSize%pieceLength == 0 {
		return pieceLength
	}
	return totalSize % pieceLength
}

// GetPieceLength returns the length of a specific piece.
func (m *Metainfo) GetPieceLength(index int) int64 {
	if index < 0 || index >= m.PieceCount() {
		return 0
	}

	if index == m.PieceCount()-1 {
		return m.getLastPieceLength()
	}

	return m.Info.PieceLength
}

// validate performs a comprehensive validation of the metainfo structure.
func (m *Metainfo) validate() error {
	if err := m.validateAnnounceURL(); err != nil {
		return err
	}
	if err := m.validateAnnounceList(); err != nil {
		return err
	}
	if err := m.Info.validate(); err != nil {
		return err
	}
	if err := m.validateConsistency(); err != nil {
		return err
	}
	return nil
}

// validateAnnounceURL validates the primary announce URL.
func (m *Metainfo) validateAnnounceURL() error {
	if m.Announce == "" {
		return newValidationError(ErrInvalidAnnounceURL, "announce", "announce URL cannot be empty")
	}
	if !validURL(m.Announce) {
		return newValidationError(ErrInvalidAnnounceURL, "announce", "invalid URL format: "+m.Announce)
	}
	return nil
}

// validateAnnounceList validates the announce-list structure.
func (m *Metainfo) validateAnnounceList() error {
	for i, tier := range m.AnnounceList {
		if len(tier) == 0 {
			continue // Empty tiers are allowed, just ignored.
		}
		for j, announceURL := range tier {
			if !validURL(announceURL) {
				return newValidationError(ErrInvalidAnnounceList, "announce-list",
					fmt.Sprintf("invalid URL at [%d][%d]: %s", i, j, announceURL))
			}
		}
	}
	return nil
}

// validURL checks if a URL is well-formed and uses an allowed scheme.
func validURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "udp":
	default:
		return false
	}
	return u.Host != ""
}

// validateConsistency validates cross-field consistency.
func (m *Metainfo) validateConsistency() error {
	if len(m.infoBytes) == 0 {
		return newValidationError(ErrInconsistentData, "info_hash", "cannot calculate info hash from empty infoBytes")
	}

	totalSize := m.TotalSize()
	if totalSize <= 0 {
		return newValidationError(ErrInconsistentData, "total_size",
			fmt.Sprintf("total size must be positive, got %d", totalSize))
	}

	pieceCount := m.PieceCount()
	if pieceCount <= 0 {
		return newValidationError(ErrInconsistentData, "piece_count",
			fmt.Sprintf("must have at least one piece, got %d", pieceCount))
	}

	// This calculation can overflow on very large numbers if not careful.
	numPieces := int64(pieceCount)
	pieceLength := m.Info.PieceLength
	expectedMinSize := (numPieces - 1) * pieceLength
	expectedMaxSize := numPieces * pieceLength

	if totalSize <= expectedMinSize || totalSize > expectedMaxSize {
		return newValidationError(ErrInconsistentData, "size_piece_mismatch",
			fmt.Sprintf("total size %d doesn't match piece layout (expected > %d and <= %d)",
				totalSize, expectedMinSize, expectedMaxSize))
	}

	return nil
}

// Validate performs validation of the Info struct.
func (i *Info) validate() error {
	if err := i.validateName(); err != nil {
		return err
	}
	if err := i.validatePieceLength(); err != nil {
		return err
	}
	if err := i.validatePieces(); err != nil {
		return err
	}
	if err := i.validateFileStructure(); err != nil {
		return err
	}
	if i.IsMultiFile() {
		if err := i.validateFiles(); err != nil {
			return err
		}
	} else {
		if err := i.validateSingleFile(); err != nil {
			return err
		}
	}
	return nil
}

// validateName validates the torrent name.
func (i *Info) validateName() error {
	if i.Name == "" {
		return newValidationError(ErrInvalidName, "name", "name cannot be empty")
	}
	if strings.ContainsAny(i.Name, "/\\\x00") {
		return newValidationError(ErrInvalidName, "name", "name cannot contain slashes or null bytes")
	}
	return nil
}

// validatePieceLength validates the piece length.
func (i *Info) validatePieceLength() error {
	if i.PieceLength <= 0 {
		return newValidationError(ErrInvalidPieceLength, "piece_length",
			fmt.Sprintf("piece length must be positive, got %d", i.PieceLength))
	}
	return nil
}

// validatePieces validates the pieces hash string.
func (i *Info) validatePieces() error {
	if len(i.Pieces) == 0 {
		return newValidationError(ErrInvalidPieces, "pieces", "pieces field cannot be empty")
	}
	if len(i.Pieces)%20 != 0 {
		return newValidationError(ErrInvalidPieces, "pieces",
			fmt.Sprintf("pieces length must be a multiple of 20, got %d", len(i.Pieces)))
	}
	return nil
}

// validateFileStructure ensures either single-file OR multi-file, not both.
func (i *Info) validateFileStructure() error {
	hasSingleFile := i.Length > 0
	hasMultiFile := len(i.Files) > 0

	if hasSingleFile && hasMultiFile {
		return newValidationError(ErrInvalidFileStructure, "structure",
			"cannot have both single-file (length) and multi-file (files) structure")
	}
	if !hasSingleFile && !hasMultiFile {
		return newValidationError(ErrInvalidFileStructure, "structure",
			"must have either single-file (length) or multi-file (files) structure")
	}
	return nil
}

// validateSingleFile validates single-file specific fields.
func (i *Info) validateSingleFile() error {
	if i.Length <= 0 {
		return newValidationError(ErrInvalidSingleFile, "length",
			fmt.Sprintf("single-file length must be positive, got %d", i.Length))
	}
	return nil
}

// validateFiles validates multi-file structure.
func (i *Info) validateFiles() error {
	if len(i.Files) == 0 && i.Length == 0 {
		return newValidationError(ErrInvalidMultiFile, "files", "torrent has no files")
	}
	pathsSeen := make(map[string]bool)
	for idx, file := range i.Files {
		if err := file.validate(idx); err != nil {
			return err
		}
		pathKey := strings.Join(file.Path, "/")
		if pathsSeen[pathKey] {
			return newValidationError(ErrInvalidMultiFile, "files", "duplicate file path: "+pathKey)
		}
		pathsSeen[pathKey] = true
	}
	return nil
}

// IsMultiFile returns true if this is a multi-file torrent.
func (i *Info) IsMultiFile() bool {
	return len(i.Files) > 0
}

// validate validates a File struct.
func (f *File) validate(fileIndex int) error {
	if f.Length < 0 {
		return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].length", fileIndex),
			fmt.Sprintf("file length cannot be negative, got %d", f.Length))
	}

	if len(f.Path) == 0 {
		return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path", fileIndex), "file path cannot be empty")
	}

	for i, component := range f.Path {
		if component == "" {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i), "path component cannot be empty")
		}
		if component == "." || component == ".." {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i), "path component cannot be '.' or '..' (security risk)")
		}
		// **FIX**: Add back the check for path component length.
		if len(component) > 255 {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i),
				fmt.Sprintf("path component too long: %d characters (max 255)", len(component)))
		}
	}
	fullPath := filepath.Join(f.Path...)
	if len(fullPath) > 4096 {
		return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path", fileIndex),
			fmt.Sprintf("full file path too long: %d characters (max 4096)", len(fullPath)))
	}
	return nil
}
