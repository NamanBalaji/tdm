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

	// Cached values for performance with thread safety
	infoHash     [20]byte
	infoHashOnce sync.Once
	infoBytes    []byte
	infoBytesMu  sync.RWMutex
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

// InfoHash returns the SHA-1 hash of the bencoded info dictionary (thread-safe cached).
func (m *Metainfo) InfoHash() [20]byte {
	m.infoHashOnce.Do(func() {
		m.infoBytesMu.RLock()
		defer m.infoBytesMu.RUnlock()
		if len(m.infoBytes) > 0 {
			m.infoHash = sha1.Sum(m.infoBytes)
		}
	})
	return m.infoHash
}

// setInfoBytes sets the info bytes (thread-safe).
func (m *Metainfo) setInfoBytes(data []byte) {
	m.infoBytesMu.Lock()
	defer m.infoBytesMu.Unlock()
	m.infoBytes = make([]byte, len(data))
	copy(m.infoBytes, data)
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
	pieceCount := m.PieceCount()
	hashes := make([][20]byte, pieceCount)

	for i := range pieceCount {
		start := i * 20
		copy(hashes[i][:], m.Info.Pieces[start:start+20])
	}

	return hashes
}

// GetPieceHash returns the hash for a specific piece index.
func (m *Metainfo) GetPieceHash(index int) ([20]byte, bool) {
	var hash [20]byte
	if index < 0 || index >= m.PieceCount() {
		return hash, false
	}

	start := index * 20
	copy(hash[:], m.Info.Pieces[start:start+20])
	return hash, true
}

// GetAnnounceURLs returns all announce URLs (primary + announce-list).
func (m *Metainfo) GetAnnounceURLs() []string {
	var urls []string
	if m.Announce != "" {
		urls = append(urls, m.Announce)
	}

	for _, tier := range m.AnnounceList {
		for _, u := range tier {
			duplicate := false
			for _, existing := range urls {
				if existing == u {
					duplicate = true
					break
				}
			}
			if !duplicate {
				urls = append(urls, u)
			}
		}
	}

	return urls
}

// GetLastPieceLength returns the length of the last piece.
func (m *Metainfo) getLastPieceLength() int64 {
	totalSize := m.TotalSize()
	pieceLength := m.Info.PieceLength

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
			return newValidationError(ErrInvalidAnnounceList, "announce-list",
				fmt.Sprintf("tier %d is empty", i))
		}

		for j, announceURL := range tier {
			if announceURL == "" {
				return newValidationError(ErrInvalidAnnounceList, "announce-list",
					fmt.Sprintf("URL at [%d][%d] is empty", i, j))
			}
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
	hash := m.InfoHash()
	if hash == [20]byte{} {
		return newValidationError(ErrInconsistentData, "info_hash", "failed to calculate info hash")
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

	expectedMinSize := int64(pieceCount-1) * m.Info.PieceLength
	expectedMaxSize := int64(pieceCount) * m.Info.PieceLength

	if totalSize < expectedMinSize || totalSize > expectedMaxSize {
		return newValidationError(ErrInconsistentData, "size_piece_mismatch",
			fmt.Sprintf("total size %d doesn't match piece layout (expected between %d and %d)",
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

	if strings.Contains(i.Name, "..") {
		return newValidationError(ErrInvalidName, "name", "name cannot contain '..' (path traversal risk)")
	}

	if strings.Contains(i.Name, "\x00") {
		return newValidationError(ErrInvalidName, "name", "name cannot contain null bytes")
	}

	return nil
}

// validatePieceLength validates the piece length.
func (i *Info) validatePieceLength() error {
	if i.PieceLength <= 0 {
		return newValidationError(ErrInvalidPieceLength, "piece_length",
			fmt.Sprintf("piece length must be positive, got %d", i.PieceLength))
	}

	if i.PieceLength < 16*1024 { // 16KB minimum
		return newValidationError(ErrInvalidPieceLength, "piece_length",
			fmt.Sprintf("piece length %d is unusually small (minimum 16KB recommended)", i.PieceLength))
	}

	if i.PieceLength > 32*1024*1024 { // 32MB maximum
		return newValidationError(ErrInvalidPieceLength, "piece_length",
			fmt.Sprintf("piece length %d is unusually large (maximum 32MB recommended)", i.PieceLength))
	}

	return nil
}

// validatePieces validates the pieces hash string.
func (i *Info) validatePieces() error {
	if len(i.Pieces) == 0 {
		return newValidationError(ErrInvalidPieces, "pieces", "pieces cannot be empty")
	}

	if len(i.Pieces)%20 != 0 {
		return newValidationError(ErrInvalidPieces, "pieces",
			fmt.Sprintf("pieces length must be multiple of 20 (SHA-1 hash size), got %d", len(i.Pieces)))
	}

	pieceCount := len(i.Pieces) / 20
	if pieceCount > 100000 {
		return newValidationError(ErrInvalidPieces, "pieces",
			fmt.Sprintf("too many pieces: %d (maximum 100,000 supported)", pieceCount))
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

	if i.MD5Sum != "" && len(i.MD5Sum) != 32 {
		return newValidationError(ErrInvalidSingleFile, "md5sum",
			fmt.Sprintf("MD5 sum must be 32 characters, got %d", len(i.MD5Sum)))
	}

	return nil
}

// validateFiles validates multi-file structure.
func (i *Info) validateFiles() error {
	if len(i.Files) == 0 {
		return newValidationError(ErrInvalidMultiFile, "files", "multi-file torrent must have at least one file")
	}

	if len(i.Files) > 50000 {
		return newValidationError(ErrInvalidMultiFile, "files",
			fmt.Sprintf("too many files: %d (maximum 50,000 supported)", len(i.Files)))
	}

	pathsSeen := make(map[string]bool)
	for idx, file := range i.Files {
		if err := file.validate(idx); err != nil {
			return err
		}

		pathKey := strings.Join(file.Path, "/")
		if pathsSeen[pathKey] {
			return newValidationError(ErrInvalidMultiFile, "files",
				"duplicate file path: "+pathKey)
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
		return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path", fileIndex),
			"file path cannot be empty")
	}

	for i, component := range f.Path {
		if component == "" {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i),
				"path component cannot be empty")
		}

		if component == "." || component == ".." {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i),
				"path component cannot be '.' or '..' (security risk)")
		}

		if strings.Contains(component, "\x00") {
			return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i),
				"path component cannot contain null bytes")
		}

		invalidChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
		for _, invalid := range invalidChars {
			if strings.Contains(component, invalid) {
				return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].path[%d]", fileIndex, i),
					fmt.Sprintf("path component contains invalid character %q", invalid))
			}
		}

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

	if f.MD5Sum != "" && len(f.MD5Sum) != 32 {
		return newValidationError(ErrInvalidFilePath, fmt.Sprintf("files[%d].md5sum", fileIndex),
			fmt.Sprintf("MD5 sum must be 32 characters, got %d", len(f.MD5Sum)))
	}

	return nil
}
