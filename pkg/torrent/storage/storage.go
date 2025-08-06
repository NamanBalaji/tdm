package storage

import "github.com/NamanBalaji/tdm/pkg/torrent/metainfo"

// Storage interface for reading/writing torrent data
type Storage interface {
	ReadBlock(b []byte, off int64) (n int, err error)
	WriteBlock(b []byte, off int64) (n int, err error)
	HashPiece(pieceIndex int, length int) (correct bool)
	Close() error
}

// Open returns a Storage object.
type Open func(mi *metainfo.Metainfo, baseDir string, blocks []int) (Storage, bool)
