package storage_test

import (
	"crypto/rand"
	"crypto/sha1"
	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
	"github.com/NamanBalaji/tdm/pkg/torrent/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFileStorage_SingleFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create single-file torrent metainfo
	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.txt",
			Len:      1024,
			PieceLen: 512,
			Pieces:   make([]byte, 40), // 2 pieces * 20 bytes each
		},
	}

	s, seeding := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	if seeding {
		t.Error("Expected seeding to be false")
	}
	defer s.Close()

	// Verify file was created
	filePath := filepath.Join(tempDir, "test.txt")
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatal("File was not created:", err)
	}
	if stat.Size() != 1024 {
		t.Errorf("Expected file size 1024, got %d", stat.Size())
	}
}

func TestOpenFileStorage_MultiFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create multi-file torrent metainfo
	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "testdir",
			PieceLen: 512,
			Pieces:   make([]byte, 40), // 2 pieces * 20 bytes each
			Files: []metainfo.File{
				{Len: 300, Path: []string{"file1.txt"}},
				{Len: 200, Path: []string{"subdir", "file2.txt"}},
				{Len: 500, Path: []string{"file3.txt"}},
			},
		},
	}

	s, seeding := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	if seeding {
		t.Error("Expected seeding to be false")
	}
	defer s.Close()

	// Verify files were created with correct sizes
	files := []struct {
		path string
		size int64
	}{
		{filepath.Join(tempDir, "testdir", "file1.txt"), 300},
		{filepath.Join(tempDir, "testdir", "subdir", "file2.txt"), 200},
		{filepath.Join(tempDir, "testdir", "file3.txt"), 500},
	}

	for _, f := range files {
		stat, err := os.Stat(f.path)
		if err != nil {
			t.Fatalf("File %s was not created: %v", f.path, err)
		}
		if stat.Size() != f.size {
			t.Errorf("File %s: expected size %d, got %d", f.path, f.size, stat.Size())
		}
	}
}

func TestReadWriteBlock_SingleFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      2048,
			PieceLen: 1024,
			Pieces:   make([]byte, 40),
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Test data
	testData := make([]byte, 512)
	rand.Read(testData)

	// Write block
	n, err := s.WriteBlock(testData, 256)
	if err != nil {
		t.Fatal("WriteBlock failed:", err)
	}
	if n != len(testData) {
		t.Errorf("WriteBlock: expected %d bytes written, got %d", len(testData), n)
	}

	// Read block back
	readData := make([]byte, 512)
	n, err = s.ReadBlock(readData, 256)
	if err != nil {
		t.Fatal("ReadBlock failed:", err)
	}
	if n != len(readData) {
		t.Errorf("ReadBlock: expected %d bytes read, got %d", len(readData), n)
	}

	// Verify data matches
	for i := range testData {
		if testData[i] != readData[i] {
			t.Errorf("Data mismatch at byte %d: wrote 0x%02x, read 0x%02x", i, testData[i], readData[i])
			break
		}
	}
}

func TestReadWriteBlock_CrossFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "testdir",
			PieceLen: 1024,
			Pieces:   make([]byte, 60), // 3 pieces
			Files: []metainfo.File{
				{Len: 500, Path: []string{"file1.bin"}},
				{Len: 600, Path: []string{"file2.bin"}},
				{Len: 400, Path: []string{"file3.bin"}},
			},
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Test writing across file boundary (file1 -> file2)
	testData := make([]byte, 200)
	rand.Read(testData)

	// Write starting at offset 450 (50 bytes in file1, 150 bytes in file2)
	n, err := s.WriteBlock(testData, 450)
	if err != nil {
		t.Fatal("WriteBlock failed:", err)
	}
	if n != len(testData) {
		t.Errorf("WriteBlock: expected %d bytes written, got %d", len(testData), n)
	}

	// Read the same span back
	readData := make([]byte, 200)
	n, err = s.ReadBlock(readData, 450)
	if err != nil {
		t.Fatal("ReadBlock failed:", err)
	}
	if n != len(readData) {
		t.Errorf("ReadBlock: expected %d bytes read, got %d", len(readData), n)
	}

	// Verify data matches
	for i := range testData {
		if testData[i] != readData[i] {
			t.Errorf("Cross-file data mismatch at byte %d: wrote 0x%02x, read 0x%02x", i, testData[i], readData[i])
			break
		}
	}
}

func TestReadWriteBlock_EdgeCases(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      1000,
			PieceLen: 500,
			Pieces:   make([]byte, 40),
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Test zero-length read/write
	n, err := s.ReadBlock([]byte{}, 0)
	if err != nil {
		t.Error("Zero-length read should not error:", err)
	}
	if n != 0 {
		t.Error("Zero-length read should return 0 bytes")
	}

	n, err = s.WriteBlock([]byte{}, 0)
	if err != nil {
		t.Error("Zero-length write should not error:", err)
	}
	if n != 0 {
		t.Error("Zero-length write should return 0 bytes")
	}

	// Test reading past end
	buf := make([]byte, 100)
	n, err = s.ReadBlock(buf, 950) // Only 50 bytes available
	if n > 50 {
		t.Errorf("Read past end: expected at most 50 bytes, got %d", n)
	}
}

func TestHashPiece_Valid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create test data and compute its hash
	pieceData := make([]byte, 512)
	rand.Read(pieceData)
	expectedHash := sha1.Sum(pieceData)

	// Create pieces array with the expected hash
	pieces := make([]byte, 20)
	copy(pieces, expectedHash[:])

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      1024,
			PieceLen: 512,
			Pieces:   pieces,
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Write the test data
	n, err := s.WriteBlock(pieceData, 0)
	if err != nil {
		t.Fatal("WriteBlock failed:", err)
	}
	if n != len(pieceData) {
		t.Errorf("WriteBlock: expected %d bytes written, got %d", len(pieceData), n)
	}

	// Verify piece hash
	if !s.HashPiece(0, 512) {
		t.Error("HashPiece should return true for valid piece")
	}
}

func TestHashPiece_Invalid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create wrong hash
	wrongHash := make([]byte, 20)
	rand.Read(wrongHash)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      1024,
			PieceLen: 512,
			Pieces:   wrongHash,
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Write some data
	testData := make([]byte, 512)
	rand.Read(testData)
	s.WriteBlock(testData, 0)

	// Verify piece hash fails
	if s.HashPiece(0, 512) {
		t.Error("HashPiece should return false for invalid piece")
	}
}

func TestHashPiece_EdgeCases(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      1024,
			PieceLen: 512,
			Pieces:   make([]byte, 40), // 2 pieces
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Test invalid piece index
	if s.HashPiece(-1, 512) {
		t.Error("HashPiece should return false for negative piece index")
	}

	if s.HashPiece(10, 512) {
		t.Error("HashPiece should return false for out-of-range piece index")
	}
}

func TestClose(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "test.bin",
			Len:      1024,
			PieceLen: 512,
			Pieces:   make([]byte, 40),
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}

	// Write some data
	testData := make([]byte, 100)
	rand.Read(testData)
	s.WriteBlock(testData, 0)

	// Close should not error
	if err := s.Close(); err != nil {
		t.Error("Close returned error:", err)
	}

	// Subsequent operations should fail gracefully
	// (This depends on the OS behavior, but generally operations on closed files fail)
}

func TestHashPiece_LargePiece(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a large piece (1MB) to test streaming hash
	pieceSize := 1024 * 1024
	pieceData := make([]byte, pieceSize)
	rand.Read(pieceData)
	expectedHash := sha1.Sum(pieceData)

	pieces := make([]byte, 20)
	copy(pieces, expectedHash[:])

	mi := &metainfo.Metainfo{
		Info: metainfo.InfoDict{
			Name:     "large.bin",
			Len:      pieceSize,
			PieceLen: pieceSize,
			Pieces:   pieces,
		},
	}

	s, _ := storage.OpenFileStorage(mi, tempDir, nil)
	if s == nil {
		t.Fatal("OpenFileStorage returned nil")
	}
	defer s.Close()

	// Write the large piece
	n, err := s.WriteBlock(pieceData, 0)
	if err != nil {
		t.Fatal("WriteBlock failed:", err)
	}
	if n != len(pieceData) {
		t.Errorf("WriteBlock: expected %d bytes written, got %d", len(pieceData), n)
	}

	// Verify piece hash (this tests the streaming implementation)
	if !s.HashPiece(0, pieceSize) {
		t.Error("HashPiece should return true for valid large piece")
	}
}
