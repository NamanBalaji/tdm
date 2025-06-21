package torrent_test

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/pkg/torrent"
	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

// Test data generation helpers
func createValidSingleFileTorrentData() []byte {
	// Create 3 pieces of 20 bytes each (SHA1 hashes)
	pieces := make([]byte, 60)
	for i := 0; i < 3; i++ {
		hash := sha1.Sum([]byte(fmt.Sprintf("piece_%d", i)))
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	info := map[string]any{
		"name":         []byte("test-file.txt"),
		"piece length": int64(16384),
		"pieces":       pieces,
		"length":       int64(49152), // 3 * 16384
	}

	torrentData := map[string]any{
		"announce":      []byte("http://tracker.example.com:8080/announce"),
		"info":          info,
		"comment":       []byte("Test torrent"),
		"created by":    []byte("Test Suite"),
		"creation date": int64(time.Now().Unix()),
	}

	encoded, _ := bencode.Encode(torrentData)
	return encoded
}

func createValidMultiFileTorrentData() []byte {
	// Create 2 pieces
	pieces := make([]byte, 40)
	for i := 0; i < 2; i++ {
		hash := sha1.Sum([]byte(fmt.Sprintf("piece_%d", i)))
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	files := []any{
		map[string]any{
			"length": int64(20000),
			"path":   []any{[]byte("dir1"), []byte("file1.txt")},
		},
		map[string]any{
			"length": int64(12768),
			"path":   []any{[]byte("dir1"), []byte("file2.txt")},
		},
	}

	info := map[string]any{
		"name":         []byte("test-multi"),
		"piece length": int64(16384),
		"pieces":       pieces,
		"files":        files,
	}

	torrentData := map[string]any{
		"announce": []byte("http://tracker.example.com:8080/announce"),
		"announce-list": []any{
			[]any{[]byte("http://tracker.example.com:8080/announce")},
			[]any{[]byte("http://backup.tracker.com:8080/announce")},
		},
		"info": info,
	}

	encoded, _ := bencode.Encode(torrentData)
	return encoded
}

func TestParseTorrent_SingleFile(t *testing.T) {
	data := createValidSingleFileTorrentData()

	metainfo, err := torrent.ParseTorrent(data)
	if err != nil {
		t.Fatalf("Failed to parse single-file torrent: %v", err)
	}

	// Test basic fields
	if metainfo.Announce != "http://tracker.example.com:8080/announce" {
		t.Errorf("Expected announce URL 'http://tracker.example.com:8080/announce', got '%s'", metainfo.Announce)
	}

	if metainfo.Info.Name != "test-file.txt" {
		t.Errorf("Expected name 'test-file.txt', got '%s'", metainfo.Info.Name)
	}

	if metainfo.Info.PieceLength != 16384 {
		t.Errorf("Expected piece length 16384, got %d", metainfo.Info.PieceLength)
	}

	if metainfo.Info.Length != 49152 {
		t.Errorf("Expected length 49152, got %d", metainfo.Info.Length)
	}

	// Test derived properties
	if metainfo.Info.IsMultiFile() {
		t.Error("Single-file torrent should not be multi-file")
	}

	if metainfo.TotalSize() != 49152 {
		t.Errorf("Expected total size 49152, got %d", metainfo.TotalSize())
	}

	if metainfo.PieceCount() != 3 {
		t.Errorf("Expected 3 pieces, got %d", metainfo.PieceCount())
	}

	// Test info hash calculation
	hash := metainfo.InfoHash()
	if hash == [20]byte{} {
		t.Error("Info hash should not be empty")
	}

	// Test piece hash extraction
	hashes := metainfo.GetPieceHashes()
	if len(hashes) != 3 {
		t.Errorf("Expected 3 piece hashes, got %d", len(hashes))
	}

	// Test individual piece hash
	firstHash, ok := metainfo.GetPieceHash(0)
	if !ok {
		t.Error("Should be able to get first piece hash")
	}
	if firstHash != hashes[0] {
		t.Error("Individual piece hash should match array")
	}

	// Test piece lengths
	for i := 0; i < metainfo.PieceCount(); i++ {
		expectedLength := int64(16384)
		actualLength := metainfo.GetPieceLength(i)
		if actualLength != expectedLength {
			t.Errorf("Expected piece %d length %d, got %d", i, expectedLength, actualLength)
		}
	}
}

func TestParseTorrent_MultiFile(t *testing.T) {
	data := createValidMultiFileTorrentData()

	metainfo, err := torrent.ParseTorrent(data)
	if err != nil {
		t.Fatalf("Failed to parse multi-file torrent: %v", err)
	}

	// Test basic fields
	if metainfo.Info.Name != "test-multi" {
		t.Errorf("Expected name 'test-multi', got '%s'", metainfo.Info.Name)
	}

	// Test multi-file specific properties
	if !metainfo.Info.IsMultiFile() {
		t.Error("Multi-file torrent should be multi-file")
	}

	if len(metainfo.Info.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(metainfo.Info.Files))
	}

	if metainfo.TotalSize() != 32768 {
		t.Errorf("Expected total size 32768, got %d", metainfo.TotalSize())
	}

	// Test announce-list
	if len(metainfo.AnnounceList) != 2 {
		t.Errorf("Expected 2 announce list tiers, got %d", len(metainfo.AnnounceList))
	}

	// Test file validation
	file1 := metainfo.Info.Files[0]
	if file1.Length != 20000 {
		t.Errorf("Expected file1 length 20000, got %d", file1.Length)
	}

	expectedPath1 := []string{"dir1", "file1.txt"}
	if len(file1.Path) != len(expectedPath1) {
		t.Errorf("Expected file1 path length %d, got %d", len(expectedPath1), len(file1.Path))
	}
	for i, component := range expectedPath1 {
		if file1.Path[i] != component {
			t.Errorf("Expected file1 path[%d] '%s', got '%s'", i, component, file1.Path[i])
		}
	}

	// Test GetAnnounceURLs
	urls := metainfo.GetAnnounceURLs()
	expectedURLs := []string{
		"http://tracker.example.com:8080/announce",
		"http://backup.tracker.com:8080/announce",
	}
	if len(urls) != len(expectedURLs) {
		t.Errorf("Expected %d URLs, got %d", len(expectedURLs), len(urls))
	}
}

func TestInfoHashCaching(t *testing.T) {
	data := createValidSingleFileTorrentData()
	metainfo, err := torrent.ParseTorrent(data)
	if err != nil {
		t.Fatalf("Failed to parse torrent: %v", err)
	}

	// Test thread safety with concurrent access
	var wg sync.WaitGroup
	numGoroutines := 100
	hashes := make([][20]byte, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hashes[idx] = metainfo.InfoHash()
		}(i)
	}

	wg.Wait()

	// All hashes should be identical (testing caching and thread safety)
	firstHash := hashes[0]
	for i, hash := range hashes {
		if hash != firstHash {
			t.Errorf("Hash mismatch at index %d: expected %x, got %x", i, firstHash, hash)
		}
	}

	// Hash should not be empty
	if firstHash == [20]byte{} {
		t.Error("Info hash should not be empty")
	}
}

func TestValidationErrors_SentinelErrors(t *testing.T) {
	testCases := []struct {
		name          string
		data          func() []byte
		expectedError error
		errorField    string
	}{
		{
			name:          "empty data",
			data:          func() []byte { return []byte{} },
			expectedError: torrent.ErrInvalidTorrentStructure,
			errorField:    "data",
		},
		{
			name: "not a dictionary",
			data: func() []byte {
				encoded, _ := bencode.Encode([]byte("not a dict"))
				return encoded
			},
			expectedError: torrent.ErrInvalidTorrentStructure,
			errorField:    "root",
		},
		{
			name: "missing announce",
			data: func() []byte {
				info := map[string]any{
					"name":         []byte("test"),
					"piece length": int64(16384),
					"pieces":       make([]byte, 20),
					"length":       int64(16384),
				}
				torrentData := map[string]any{
					"info": info,
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidAnnounceURL,
			errorField:    "announce",
		},
		{
			name: "missing info",
			data: func() []byte {
				torrentData := map[string]any{
					"announce": []byte("http://tracker.example.com"),
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidInfoDict,
			errorField:    "info",
		},
		{
			name: "invalid pieces length",
			data: func() []byte {
				info := map[string]any{
					"name":         []byte("test"),
					"piece length": int64(16384),
					"pieces":       []byte("123456789"), // Not multiple of 20
					"length":       int64(16384),
				}
				torrentData := map[string]any{
					"announce": []byte("http://tracker.example.com"),
					"info":     info,
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidPieces,
			errorField:    "pieces",
		},
		{
			name: "both single and multi file",
			data: func() []byte {
				info := map[string]any{
					"name":         []byte("test"),
					"piece length": int64(16384),
					"pieces":       make([]byte, 20),
					"length":       int64(16384),
					"files": []any{
						map[string]any{
							"length": int64(100),
							"path":   []any{[]byte("file.txt")},
						},
					},
				}
				torrentData := map[string]any{
					"announce": []byte("http://tracker.example.com"),
					"info":     info,
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidFileStructure,
			errorField:    "structure",
		},
		{
			name: "invalid announce URL",
			data: func() []byte {
				info := map[string]any{
					"name":         []byte("test"),
					"piece length": int64(16384),
					"pieces":       make([]byte, 20),
					"length":       int64(16384),
				}
				torrentData := map[string]any{
					"announce": []byte("not-a-url"),
					"info":     info,
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidAnnounceURL,
			errorField:    "announce",
		},
		{
			name: "dangerous file path",
			data: func() []byte {
				info := map[string]any{
					"name":         []byte("test"),
					"piece length": int64(16384),
					"pieces":       make([]byte, 20),
					"files": []any{
						map[string]any{
							"length": int64(100),
							"path": []any{
								[]byte(".."),
								[]byte("etc"),
								[]byte("passwd"),
							},
						},
					},
				}
				torrentData := map[string]any{
					"announce": []byte("http://tracker.example.com"),
					"info":     info,
				}
				encoded, _ := bencode.Encode(torrentData)
				return encoded
			},
			expectedError: torrent.ErrInvalidFilePath,
			errorField:    "files[0].path[0]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data()
			_, err := torrent.ParseTorrent(data)
			if err == nil {
				t.Errorf("Expected error for %s", tc.name)
				return
			}

			// Test that error can be unwrapped to sentinel error
			if !errors.Is(err, tc.expectedError) {
				t.Errorf("Expected error type %v, got %v", tc.expectedError, err)
			}

			// Test that error contains expected field
			if !strings.Contains(err.Error(), tc.errorField) {
				t.Errorf("Expected error to contain field '%s', got '%s'", tc.errorField, err.Error())
			}

			// Test ValidationError type
			var validationErr *torrent.ValidationError
			if !errors.As(err, &validationErr) {
				t.Errorf("Expected ValidationError, got %T", err)
			} else {
				if validationErr.Type != tc.expectedError {
					t.Errorf("Expected ValidationError.Type %v, got %v", tc.expectedError, validationErr.Type)
				}
			}
		})
	}
}

func TestGetPieceLength(t *testing.T) {
	// Create torrent with uneven last piece
	pieces := make([]byte, 40) // 2 pieces
	for i := 0; i < 2; i++ {
		hash := sha1.Sum([]byte(fmt.Sprintf("piece_%d", i)))
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	info := map[string]any{
		"name":         []byte("test-uneven.txt"),
		"piece length": int64(16384),
		"pieces":       pieces,
		"length":       int64(20000), // 1 full piece + partial piece
	}

	torrentData := map[string]any{
		"announce": []byte("http://tracker.example.com"),
		"info":     info,
	}

	encoded, _ := bencode.Encode(torrentData)
	metainfo, err := torrent.ParseTorrent(encoded)
	if err != nil {
		t.Fatalf("Failed to parse torrent: %v", err)
	}

	// Test normal piece
	length0 := metainfo.GetPieceLength(0)
	if length0 != 16384 {
		t.Errorf("Expected piece 0 length 16384, got %d", length0)
	}

	// Test last piece (should be smaller)
	lastLength := metainfo.GetPieceLength(1)
	expectedLastLength := int64(20000 - 16384) // 3616
	if lastLength != expectedLastLength {
		t.Errorf("Expected last piece length %d, got %d", expectedLastLength, lastLength)
	}

	// Test invalid piece index
	invalidLength := metainfo.GetPieceLength(-1)
	if invalidLength != 0 {
		t.Errorf("Expected 0 for invalid piece index, got %d", invalidLength)
	}

	invalidLength2 := metainfo.GetPieceLength(10)
	if invalidLength2 != 0 {
		t.Errorf("Expected 0 for out-of-bounds piece index, got %d", invalidLength2)
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("very long file path", func(t *testing.T) {
		longComponent := strings.Repeat("a", 300) // Longer than 255 chars

		info := map[string]any{
			"name":         []byte("test"),
			"piece length": int64(16384),
			"pieces":       make([]byte, 20),
			"files": []any{
				map[string]any{
					"length": int64(100),
					"path":   []any{[]byte(longComponent)},
				},
			},
		}
		torrentData := map[string]any{
			"announce": []byte("http://tracker.example.com"),
			"info":     info,
		}
		encoded, _ := bencode.Encode(torrentData)

		_, err := torrent.ParseTorrent(encoded)
		if err == nil {
			t.Error("Should reject very long path components")
		}
		if !errors.Is(err, torrent.ErrInvalidFilePath) {
			t.Errorf("Expected ErrInvalidFilePath, got %v", err)
		}
	})

	t.Run("null bytes in name", func(t *testing.T) {
		info := map[string]any{
			"name":         []byte("test\x00file"),
			"piece length": int64(16384),
			"pieces":       make([]byte, 20),
			"length":       int64(16384),
		}
		torrentData := map[string]any{
			"announce": []byte("http://tracker.example.com"),
			"info":     info,
		}
		encoded, _ := bencode.Encode(torrentData)

		_, err := torrent.ParseTorrent(encoded)
		if err == nil {
			t.Error("Should reject null bytes in name")
		}
		if !errors.Is(err, torrent.ErrInvalidName) {
			t.Errorf("Expected ErrInvalidName, got %v", err)
		}
	})
}

func TestLargeFileHandling(t *testing.T) {
	// Create a torrent with many pieces to test memory efficiency
	pieceCount := 1000
	pieces := make([]byte, pieceCount*20)
	for i := 0; i < pieceCount; i++ {
		hash := sha1.Sum([]byte(fmt.Sprintf("piece_%d", i)))
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	info := map[string]any{
		"name":         []byte("large-file.bin"),
		"piece length": int64(1048576), // 1MB pieces
		"pieces":       pieces,
		"length":       int64(pieceCount) * 1048576,
	}

	torrentData := map[string]any{
		"announce": []byte("http://tracker.example.com"),
		"info":     info,
	}

	encoded, _ := bencode.Encode(torrentData)
	metainfo, err := torrent.ParseTorrent(encoded)
	if err != nil {
		t.Fatalf("Failed to parse large torrent: %v", err)
	}

	if metainfo.PieceCount() != pieceCount {
		t.Errorf("Expected %d pieces, got %d", pieceCount, metainfo.PieceCount())
	}

	if metainfo.TotalSize() != int64(pieceCount)*1048576 {
		t.Errorf("Expected total size %d, got %d", int64(pieceCount)*1048576, metainfo.TotalSize())
	}

	// Test that we can access individual piece hashes efficiently
	hash, ok := metainfo.GetPieceHash(500)
	if !ok {
		t.Error("Should be able to get piece hash 500")
	}
	if hash == [20]byte{} {
		t.Error("Piece hash should not be empty")
	}
}

func TestAnnounceListParsing(t *testing.T) {
	info := map[string]any{
		"name":         []byte("test"),
		"piece length": int64(16384),
		"pieces":       make([]byte, 20),
		"length":       int64(16384),
	}

	torrentData := map[string]any{
		"announce": []byte("http://primary.tracker.com"),
		"announce-list": []any{
			[]any{[]byte("http://primary.tracker.com")},
			[]any{
				[]byte("http://backup1.tracker.com"),
				[]byte("http://backup2.tracker.com"),
			},
			[]any{[]byte("udp://fallback.tracker.com:1337")},
		},
		"info": info,
	}

	encoded, _ := bencode.Encode(torrentData)
	metainfo, err := torrent.ParseTorrent(encoded)
	if err != nil {
		t.Fatalf("Failed to parse torrent: %v", err)
	}

	urls := metainfo.GetAnnounceURLs()
	expectedURLs := []string{
		"http://primary.tracker.com",
		"http://backup1.tracker.com",
		"http://backup2.tracker.com",
		"udp://fallback.tracker.com:1337",
	}

	if len(urls) != len(expectedURLs) {
		t.Errorf("Expected %d URLs, got %d", len(expectedURLs), len(urls))
	}

	for _, expected := range expectedURLs {
		found := false
		for _, actual := range urls {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected URL '%s' not found in announce URLs", expected)
		}
	}
}
