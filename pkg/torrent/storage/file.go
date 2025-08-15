package storage

// The storage package provides an implementation for persisting torrent
// data to disk. It exposes a Storage interface which abstracts away the
// details of reading and writing data across one or more files and
// verifying piece hashes. This file contains the default file backed
// implementation used by the torrent client.

import (
	"bytes"
	"crypto/sha1"
	"io"
	"os"
	"path/filepath"

	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
)

// hashBufferSize defines the buffer size used when hashing a piece. A larger
// buffer improves hashing throughput by reducing the number of read calls.
const hashBufferSize = 32 * 1024 // 32 KiB

// fileStorage implements the Storage interface using one or more files on
// disk. For multi‑file torrents the data spans multiple files; offsets
// track where each file begins within the overall torrent data.
type fileStorage struct {
	files    []*os.File
	offsets  []int64 // absolute byte offset where each file starts
	fileLens []int64 // length of each file
	mi       *metainfo.Metainfo
}

// OpenFileStorage creates a new file based storage backend. The caller
// supplies the parsed Metainfo and a base directory to write files into.
// The returned Storage abstracts over one or more files depending on
// whether the torrent is single or multi file. The second return value
// indicates whether the torrent is already fully present on disk (seed). At
// present this implementation always returns false since seeding support
// will be added later.
func OpenFileStorage(mi *metainfo.Metainfo, baseDir string, _ []int) (Storage, bool) {
	var (
		files       []*os.File
		offsets     []int64
		fileLens    []int64
		totalOffset int64
	)

	// Build file list, offsets and lengths.
	// Single file torrents place data into one file named after the info
	// dictionary's name field. For multi file torrents the info.Name
	// represents a directory and each file's Path specifies its relative
	// location inside that directory.

	if mi.Info.Len > 0 { // single file torrent
		name := filepath.Join(baseDir, mi.Info.Name)

		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, false
		}

		// Truncate to expected size. This reserves space on disk and allows
		// piece writes beyond the current end of file. Without truncation
		// writes past EOF would grow the file but might fragment data.
		if err := f.Truncate(int64(mi.Info.Len)); err != nil {
			f.Close()
			return nil, false
		}

		files = []*os.File{f}
		offsets = []int64{0}
		fileLens = []int64{int64(mi.Info.Len)}
	} else { // multi file torrent
		root := filepath.Join(baseDir, mi.Info.Name)

		for _, fi := range mi.Info.Files {
			path := filepath.Join(root, filepath.Join(fi.Path...))

			// Create directory if needed
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				// Close any files we've already opened
				for _, f := range files {
					f.Close()
				}

				return nil, false
			}

			f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				for _, f := range files {
					f.Close()
				}

				return nil, false
			}

			// Truncate each file to its own length
			if err := f.Truncate(int64(fi.Len)); err != nil {
				f.Close()

				for _, f := range files {
					f.Close()
				}

				return nil, false
			}

			files = append(files, f)
			offsets = append(offsets, totalOffset)
			fileLens = append(fileLens, int64(fi.Len))
			totalOffset += int64(fi.Len)
		}
	}

	// Verify file lengths match what we expect. If the file already existed
	// the actual size may differ from what's specified in the torrent; this
	// implementation assumes the caller is downloading from scratch so the
	// existing file size is respected to avoid truncating partial data.
	for i, f := range files {
		stat, err := f.Stat()
		if err != nil {
			for _, f := range files {
				f.Close()
			}

			return nil, false
		}

		if stat.Size() != fileLens[i] {
			fileLens[i] = stat.Size()
		}
	}

	return &fileStorage{
		files:    files,
		offsets:  offsets,
		fileLens: fileLens,
		mi:       mi,
	}, false // currently we always start as a leech; seeding detection can be added later
}

// ReadBlock reads data starting at an absolute offset across potentially
// multiple files. It fills the supplied buffer and returns the number of
// bytes read. If fewer bytes are returned than requested it may indicate
// end‑of‑file.
func (fs *fileStorage) ReadBlock(b []byte, off int64) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	totalRead := 0
	remaining := len(b)
	currentOffset := off

	for remaining > 0 {
		fileIdx, relativeOffset := fs.findFileAndOffset(currentOffset)
		if fileIdx == -1 {
			break // offset beyond end of torrent
		}

		// Determine how many bytes can be read from this file
		bytesAvailableInFile := fs.fileLens[fileIdx] - relativeOffset
		if bytesAvailableInFile <= 0 {
			break
		}

		bytesToRead := int64(remaining)
		if bytesToRead > bytesAvailableInFile {
			bytesToRead = bytesAvailableInFile
		}

		// Read from current file
		n, err := fs.files[fileIdx].ReadAt(b[totalRead:totalRead+int(bytesToRead)], relativeOffset)
		totalRead += n
		remaining -= n
		currentOffset += int64(n)

		if err != nil && err != io.EOF {
			return totalRead, err
		}

		if n < int(bytesToRead) {
			break // short read, likely EOF
		}
	}

	return totalRead, nil
}

// WriteBlock writes data starting at an absolute offset across potentially
// multiple files. It writes the contents of b into the underlying files and
// returns the number of bytes written. A short write indicates that no
// further writes are possible at this offset (e.g. past the end of the
// torrent).
func (fs *fileStorage) WriteBlock(b []byte, off int64) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	totalWritten := 0
	remaining := len(b)
	currentOffset := off

	for remaining > 0 {
		fileIdx, relativeOffset := fs.findFileAndOffset(currentOffset)
		if fileIdx == -1 {
			break // offset beyond end of torrent
		}

		// Determine how many bytes can be written to this file
		bytesAvailableInFile := fs.fileLens[fileIdx] - relativeOffset
		if bytesAvailableInFile <= 0 {
			break
		}

		bytesToWrite := int64(remaining)
		if bytesToWrite > bytesAvailableInFile {
			bytesToWrite = bytesAvailableInFile
		}

		// Write to current file
		n, err := fs.files[fileIdx].WriteAt(b[totalWritten:totalWritten+int(bytesToWrite)], relativeOffset)
		totalWritten += n
		remaining -= n
		currentOffset += int64(n)

		if err != nil {
			return totalWritten, err
		}

		if n < int(bytesToWrite) {
			break // short write
		}
	}

	return totalWritten, nil
}

// HashPiece verifies the SHA‑1 hash for a given piece index matches the
// expected value in the torrent's piece list. It streams data from the
// underlying storage into a SHA‑1 hash to avoid loading the entire piece
// into memory at once. If the piece length is shorter than a full piece
// because it's the last piece the caller must pass the length explicitly.
func (fs *fileStorage) HashPiece(pieceIndex int, length int) bool {
	if pieceIndex < 0 || pieceIndex*20 >= len(fs.mi.Info.Pieces) {
		return false
	}

	h := sha1.New()
	start := int64(pieceIndex) * int64(fs.mi.Info.PieceLen)
	remaining := length
	currentOffset := start
	buf := make([]byte, hashBufferSize)

	for remaining > 0 {
		readSize := hashBufferSize
		if remaining < readSize {
			readSize = remaining
		}

		n, err := fs.ReadBlock(buf[:readSize], currentOffset)
		if n > 0 {
			h.Write(buf[:n])
			remaining -= n
			currentOffset += int64(n)
		}

		if err != nil || n == 0 {
			break
		}
	}

	expected := fs.mi.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actual := h.Sum(nil)

	return bytes.Equal(actual, expected)
}

// Close closes all underlying file descriptors. The last encountered error
// (if any) is returned.
func (fs *fileStorage) Close() error {
	var lastErr error

	for _, f := range fs.files {
		if err := f.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// findFileAndOffset returns the file index and relative offset for an
// absolute position within the torrent. If the offset lies beyond the end
// of the torrent the returned index is ‑1.
func (fs *fileStorage) findFileAndOffset(abs int64) (fileIdx int, relativeOffset int64) {
	for i, offset := range fs.offsets {
		if abs >= offset && abs < offset+fs.fileLens[i] {
			return i, abs - offset
		}
	}

	return -1, 0
}
