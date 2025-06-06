package torrent

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Magnet link errors.
var (
	ErrInvalidMagnet     = errors.New("invalid magnet link")
	ErrNoInfoHash        = errors.New("magnet link missing info hash")
	ErrInvalidInfoHash   = errors.New("invalid info hash in magnet link")
	ErrInvalidTrackerURL = errors.New("invalid tracker URL in magnet link")
)

// MagnetLink represents a parsed magnet link.
type MagnetLink struct {
	// InfoHash is the torrent's info hash (required)
	InfoHash [20]byte
	// InfoHashHex is the hex representation of InfoHash
	InfoHashHex string
	// DisplayName is the suggested name for the download
	DisplayName string
	// Trackers is a list of tracker URLs
	Trackers []string
	// PeerAddresses is a list of peer addresses for direct connection
	PeerAddresses []string
	// WebSeeds is a list of web seed URLs
	WebSeeds []string
	// ExactLength is the exact length in bytes (if known)
	ExactLength int64
	// ExactSource is a URL to the exact file
	ExactSource string
}

// ParseMagnet parses a magnet link string.
func ParseMagnet(magnetURI string) (*MagnetLink, error) {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return nil, fmt.Errorf("%w: must start with 'magnet:?'", ErrInvalidMagnet)
	}

	// Parse the query string
	queryString := magnetURI[8:] // Skip "magnet:?"
	params, err := url.ParseQuery(queryString)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidMagnet, err)
	}

	magnet := &MagnetLink{
		Trackers:      make([]string, 0),
		PeerAddresses: make([]string, 0),
		WebSeeds:      make([]string, 0),
	}

	// Parse exact topic (xt) - this contains the info hash
	xt := params.Get("xt")
	if xt == "" {
		return nil, ErrNoInfoHash
	}

	if err := parseExactTopic(xt, magnet); err != nil {
		return nil, err
	}

	if dn := params.Get("dn"); dn != "" {
		magnet.DisplayName = dn
	}

	// Parse exact length (xl)
	if xl := params.Get("xl"); xl != "" {
		length, err := strconv.ParseInt(xl, 10, 64)
		if err == nil && length >= 0 {
			magnet.ExactLength = length
		}
	}

	// Parse trackers (tr) - can have multiple
	for _, tr := range params["tr"] {
		if tr != "" {
			// Validate tracker URL
			if _, err := url.Parse(tr); err == nil {
				magnet.Trackers = append(magnet.Trackers, tr)
			}
		}
	}

	// Parse peer addresses (x.pe) - can have multiple
	for _, pe := range params["x.pe"] {
		if pe != "" {
			magnet.PeerAddresses = append(magnet.PeerAddresses, pe)
		}
	}

	// Parse web seeds (ws) - can have multiple
	for _, ws := range params["ws"] {
		if ws != "" {
			if _, err := url.Parse(ws); err == nil {
				magnet.WebSeeds = append(magnet.WebSeeds, ws)
			}
		}
	}

	// Parse exact source (xs)
	if xs := params.Get("xs"); xs != "" {
		if _, err := url.Parse(xs); err == nil {
			magnet.ExactSource = xs
		}
	}

	return magnet, nil
}

// parseExactTopic parses the xt parameter which contains the info hash.
func parseExactTopic(xt string, magnet *MagnetLink) error {
	// BitTorrent info hash format: urn:btih:<info-hash>
	const btihPrefix = "urn:btih:"
	if !strings.HasPrefix(xt, btihPrefix) {
		return fmt.Errorf("%w: xt must start with '%s'", ErrInvalidMagnet, btihPrefix)
	}

	hashStr := xt[len(btihPrefix):]
	if hashStr == "" {
		return ErrNoInfoHash
	}

	// Info hash can be in hex (40 chars) or base32 (32 chars)
	switch len(hashStr) {
	case 40: // Hex encoding
		hash, err := hex.DecodeString(hashStr)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInfoHash, err)
		}
		if len(hash) != 20 {
			return fmt.Errorf("%w: wrong length", ErrInvalidInfoHash)
		}
		copy(magnet.InfoHash[:], hash)
		magnet.InfoHashHex = hashStr

	case 32: // Base32 encoding
		hash, err := decodeBase32(hashStr)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidInfoHash, err)
		}
		if len(hash) != 20 {
			return fmt.Errorf("%w: wrong length", ErrInvalidInfoHash)
		}
		copy(magnet.InfoHash[:], hash)
		magnet.InfoHashHex = hex.EncodeToString(hash)

	default:
		return fmt.Errorf("%w: hash must be 40 chars (hex) or 32 chars (base32)", ErrInvalidInfoHash)
	}

	return nil
}

// decodeBase32 decodes a base32 string (BitTorrent uses a custom base32 alphabet).
func decodeBase32(s string) ([]byte, error) {
	const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

	s = strings.ToUpper(s)

	outputLen := len(s) * 5 / 8
	output := make([]byte, outputLen)

	var buffer uint64
	var bitsInBuffer int
	outputPos := 0

	for _, char := range s {
		val := strings.IndexRune(base32Alphabet, char)
		if val == -1 {
			return nil, fmt.Errorf("invalid base32 character: %c", char)
		}

		buffer = (buffer << 5) | uint64(val)
		bitsInBuffer += 5

		for bitsInBuffer >= 8 {
			bitsInBuffer -= 8
			b := byte(buffer >> uint(bitsInBuffer))
			if outputPos < len(output) {
				output[outputPos] = b
				outputPos++
			}
			buffer &= (1 << uint(bitsInBuffer)) - 1
		}
	}

	return output, nil
}

// ToMetadata creates a minimal Metadata structure from a magnet link
// Note: This will only have the info hash and trackers. The actual torrent
// metadata needs to be downloaded from peers.
func (m *MagnetLink) ToMetadata() *Metadata {
	metadata := &Metadata{
		InfoHash:    m.InfoHash,
		InfoHashHex: m.InfoHashHex,
	}

	// Set primary tracker if available
	if len(m.Trackers) > 0 {
		metadata.Announce = m.Trackers[0]

		// Create announce list with all trackers
		metadata.AnnounceList = make([][]string, len(m.Trackers))
		for i, tracker := range m.Trackers {
			metadata.AnnounceList[i] = []string{tracker}
		}
	}

	// Set name if available
	if m.DisplayName != "" {
		metadata.Info.Name = m.DisplayName
	}

	return metadata
}
