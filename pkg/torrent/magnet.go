package torrent

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MagnetLink represents a parsed magnet link.
type MagnetLink struct {
	InfoHash    [20]byte
	DisplayName string
	Trackers    []string
	ExactLength int64
	ExactSource string
}

// ParseMagnetLink parses a magnet URI.
func ParseMagnetLink(magnetURI string) (*MagnetLink, error) {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return nil, fmt.Errorf("invalid magnet link format")
	}

	u, err := url.Parse(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse magnet URI: %w", err)
	}

	params := u.Query()
	ml := &MagnetLink{}

	// Parse exact topic (xt) - info hash
	xt := params.Get("xt")
	if xt == "" {
		return nil, fmt.Errorf("missing xt (exact topic) parameter")
	}

	if !strings.HasPrefix(xt, "urn:btih:") {
		return nil, fmt.Errorf("unsupported xt format: %s", xt)
	}

	hashStr := strings.TrimPrefix(xt, "urn:btih:")
	hashBytes, err := hex.DecodeString(hashStr)
	if err != nil {
		return nil, fmt.Errorf("invalid info hash: %w", err)
	}

	if len(hashBytes) != 20 {
		return nil, fmt.Errorf("info hash must be 20 bytes, got %d", len(hashBytes))
	}

	copy(ml.InfoHash[:], hashBytes)

	// Parse display name (dn)
	if dn := params.Get("dn"); dn != "" {
		ml.DisplayName = dn
	}

	// Parse trackers (tr)
	ml.Trackers = params["tr"]

	// Parse exact length (xl)
	if xl := params.Get("xl"); xl != "" {
		length, err := strconv.ParseInt(xl, 10, 64)
		if err == nil {
			ml.ExactLength = length
		}
	}

	// Parse exact source (xs)
	if xs := params.Get("xs"); xs != "" {
		ml.ExactSource = xs
	}

	return ml, nil
}

// ToMagnetURI converts the magnet link back to a URI string.
func (ml *MagnetLink) ToMagnetURI() string {
	params := url.Values{}

	// Add info hash
	params.Set("xt", "urn:btih:"+hex.EncodeToString(ml.InfoHash[:]))

	// Add display name
	if ml.DisplayName != "" {
		params.Set("dn", ml.DisplayName)
	}

	// Add trackers
	for _, tracker := range ml.Trackers {
		params.Add("tr", tracker)
	}

	// Add exact length
	if ml.ExactLength > 0 {
		params.Set("xl", strconv.FormatInt(ml.ExactLength, 10))
	}

	// Add exact source
	if ml.ExactSource != "" {
		params.Set("xs", ml.ExactSource)
	}

	return "magnet:?" + params.Encode()
}
