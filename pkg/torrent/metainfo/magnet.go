package metainfo

import (
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

// Magnet represents a parsed magnet link. It exposes the 20 byte
// info-hash, an optional display name and a slice of tracker URLs. A
// magnet link may contain zero or more trackers; absence of trackers
// means peers must be discovered via DHT or other mechanisms.
type Magnet struct {
	InfoHash    [20]byte
	DisplayName string
	Trackers    []string
}

// ParseMagnet parses a magnet link string and returns a Magnet
// structure. It validates the scheme and extracts xt (info-hash), dn
// (display name) and tr (tracker) parameters. Both hex and base32
// encoded info-hashes are supported.
func ParseMagnet(raw string) (*Magnet, error) {
	if !strings.HasPrefix(raw, "magnet:") {
		return nil, errors.New("not a magnet link")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	q := u.Query()

	xt := q.Get("xt")
	if !strings.HasPrefix(xt, "urn:btih:") {
		return nil, errors.New("missing or invalid xt parameter")
	}

	xt = strings.TrimPrefix(xt, "urn:btih:")

	hash, err := decodeHash(xt)
	if err != nil {
		return nil, err
	}

	var trackers []string

	for _, tr := range q["tr"] {
		if tr != "" {
			trackers = append(trackers, tr)
		}
	}

	return &Magnet{
		InfoHash:    hash,
		DisplayName: q.Get("dn"),
		Trackers:    trackers,
	}, nil
}

// decodeHash decodes either a 40 character hex or a 32 character base32
// encoded info-hash into a 20 byte array.
func decodeHash(s string) ([20]byte, error) {
	var out [20]byte

	switch len(s) {
	case 40:
		_, err := hex.Decode(out[:], []byte(s))
		return out, err
	case 32:
		_, err := base32.StdEncoding.Decode(out[:], []byte(strings.ToUpper(s)))
		return out, err
	default:
		return out, errors.New("info_hash length invalid")
	}
}
