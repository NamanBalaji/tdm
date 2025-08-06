package storage

import (
	"bytes"
	"crypto/sha1"
	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
	"io"
	"os"
	"path/filepath"
)

const hashBufferSize = 32 * 1024 // 32 KiB

type fileStorage struct {
	files    []*os.File
	offsets  []int64 // absolute byte offset where each file starts
	fileLens []int64 // length of each file
	mi       *metainfo.Metainfo
}

// OpenFileStorage creates a new file-based storage implementation
func OpenFileStorage(mi *metainfo.Metainfo, baseDir string, _ []int) (Storage, bool) {
	var files []*os.File
	var offsets []int64
	var fileLens []int64
	var totalOffset int64

	// Build file list, offsets, and lengths
	if mi.Info.Len > 0 { // single file torrent
		name := filepath.Join(baseDir, mi.Info.Name)
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, false
		}

		// Truncate to expected size
		if err := f.Truncate(int64(mi.Info.Len)); err != nil {
			f.Close()
			return nil, false
		}

		files = []*os.File{f}
		offsets = []int64{0}
		fileLens = []int64{int64(mi.Info.Len)}
	} else { // multi-file torrent
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
				// Close any files we've already opened
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

	// Verify file lengths match what we expect
	for i, f := range files {
		stat, err := f.Stat()
		if err != nil {
			for _, f := range files {
				f.Close()
			}
			return nil, false
		}
		if stat.Size() != fileLens[i] {
			fileLens[i] = stat.Size() // sync with actual file size
		}
	}

	return &fileStorage{
		files:    files,
		offsets:  offsets,
		fileLens: fileLens,
		mi:       mi,
	}, false // seed = false for now
}

// ReadBlock reads data starting at absolute offset across potentially multiple files
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

		// Calculate how much we can read from this file
		bytesAvailableInFile := fs.fileLens[fileIdx] - relativeOffset
		if bytesAvailableInFile <= 0 {
			break // no more data in this file
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
			break // short read, probably EOF
		}
	}

	return totalRead, nil
}

// WriteBlock writes data starting at absolute offset across potentially multiple files
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

		// Calculate how much we can write to this file
		bytesAvailableInFile := fs.fileLens[fileIdx] - relativeOffset
		if bytesAvailableInFile <= 0 {
			break // no space in this file
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

// HashPiece verifies a piece hash using streaming reads
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

	// Compare with expected hash
	expected := fs.mi.Info.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actual := h.Sum(nil)
	return bytes.Equal(actual, expected)
}

// Close closes all open files
func (fs *fileStorage) Close() error {
	var lastErr error
	for _, f := range fs.files {
		if err := f.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// findFileAndOffset returns the file index and relative offset for an absolute position
func (fs *fileStorage) findFileAndOffset(abs int64) (fileIdx int, relativeOffset int64) {
	for i, offset := range fs.offsets {
		if abs >= offset && abs < offset+fs.fileLens[i] {
			return i, abs - offset
		}
	}
	return -1, 0 // not found
}
