package storage

import "github.com/NamanBalaji/tdm/pkg/torrent/metainfo"

// Storage defines the interface for reading and writing torrent data. It
// abstracts away the underlying storage medium (disk, memory, etc.) so the
// downloader can remain agnostic. A Storage implementation should be
// goroutine safe if it will be accessed concurrently.
//
// ReadBlock and WriteBlock operate on absolute offsets within the torrent's
// overall byte stream. For multi‑file torrents the implementation must
// transparently read and write across file boundaries.
//
// HashPiece calculates and verifies the SHA‑1 hash for a piece. The caller
// passes the piece index and the length of the piece (which may be shorter
// for the final piece).
type Storage interface {
	ReadBlock(b []byte, off int64) (n int, err error)
	WriteBlock(b []byte, off int64) (n int, err error)
	HashPiece(pieceIndex int, length int) (correct bool)
	Close() error
}

// Open defines a factory function that returns a Storage implementation for
// the given torrent. The blocks slice can be used to resume partially
// downloaded torrents by indicating which piece indices are already
// complete. The bool return value indicates whether the torrent is fully
// present on disk (i.e. seeding). Future implementations may use this to
// avoid re‑downloading existing data.
type Open func(mi *metainfo.Metainfo, baseDir string, blocks []int) (Storage, bool)
