package metainfo

import (
	"crypto/sha1"
	"errors"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// Metainfo represents the top-level structure of a torrent file. It
// contains announce URLs, optional comments and creator information,
// and the info dictionary describing file(s) included in the torrent.
// The InfoHash field is populated after parsing and validation.
type Metainfo struct {
	Announce     string     `bencode:"announce"`
	AnnounceList [][]string `bencode:"announce-list" empty:"omit"`
	Comment      string     `bencode:"comment" empty:"omit"`
	CreatedBy    string     `bencode:"created by" empty:"omit"`
	CreationDate int        `bencode:"creation date" empty:"omit"`
	Encoding     string     `bencode:"encoding" empty:"omit"`
	Info         InfoDict   `bencode:"info"`
	InfoHash     [20]byte
}

// InfoDict represents the 'info' dictionary of a torrent. It
// describes either a single file (via Len) or multiple files. Each
// piece in the torrent is hashed and concatenated into the Pieces
// field.
type InfoDict struct {
	Files    []File `bencode:"files" empty:"omit"`
	Len      int    `bencode:"length" empty:"omit"`
	Md5      []byte `bencode:"md5sum" empty:"omit"`
	Name     string `bencode:"name"`
	PieceLen int    `bencode:"piece length"`
	Pieces   []byte `bencode:"pieces"`
	Private  int    `bencode:"private" empty:"omit"`
}

// File represents an individual file in a multi-file torrent. Path
// components are stored as a slice of strings.
type File struct {
	Len  int      `bencode:"length"`
	Path []string `bencode:"path"`
	Md5  []byte   `bencode:"md5sum" empty:"omit"`
}

// ParseMetainfoFromBytes unmarshals a bencoded torrent file into a
// Metainfo structure, validates required fields and computes the
// info-hash. The returned Metainfo must be further validated before
// use.
func ParseMetainfoFromBytes(data []byte) (*Metainfo, error) {
	mi := &Metainfo{}
	if err := bencode.Unmarshal(data, mi); err != nil {
		return nil, err
	}

	if err := mi.validate(); err != nil {
		return nil, err
	}

	infoBytes, err := bencode.Marshal(mi.Info)
	if err != nil {
		return nil, err
	}

	mi.InfoHash = sha1.Sum(infoBytes)

	return mi, nil
}

// validate performs basic structural checks on the metainfo.
func (m *Metainfo) validate() error {
	if m.Announce == "" && len(m.AnnounceList) == 0 {
		return errors.New("no announce URL found")
	}

	if len(m.Info.Pieces)%20 != 0 {
		return errors.New("pieces string length not multiple of 20")
	}

	if m.Info.PieceLen <= 0 {
		return errors.New("invalid piece length")
	}
	// Exactly one of single-file length or multi-file array must be present.
	if (m.Info.Len == 0) == (len(m.Info.Files) == 0) {
		return errors.New("exactly one of length or files must be present")
	}

	return nil
}
