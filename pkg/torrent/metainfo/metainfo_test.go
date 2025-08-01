package metainfo_test

import (
	"crypto/sha1"
	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
	"reflect"
	"strings"
	"testing"
)

func TestParseMetainfoFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		data    func() []byte
		want    func() *metainfo.Metainfo
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid single file torrent",
			data: func() []byte {
				pieces := make([]byte, 40)
				copy(pieces[0:20], []byte("12345678901234567890"))
				copy(pieces[20:40], []byte("abcdefghijklmnopqrst"))

				info := map[string]any{
					"name":         "test.txt",
					"piece length": 32768,
					"pieces":       string(pieces),
					"length":       65536,
					"private":      0,
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"comment":  "Test torrent",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			want: func() *metainfo.Metainfo {
				pieces := make([]byte, 40)
				copy(pieces[0:20], []byte("12345678901234567890"))
				copy(pieces[20:40], []byte("abcdefghijklmnopqrst"))

				return &metainfo.Metainfo{
					Announce: "http://tracker.example.com/announce",
					Comment:  "Test torrent",
					Info: metainfo.InfoDict{
						Name:     "test.txt",
						PieceLen: 32768,
						Pieces:   pieces,
						Len:      65536,
						Private:  0,
					},
				}
			},
		},
		{
			name: "valid multi file torrent",
			data: func() []byte {
				pieces := make([]byte, 20)
				copy(pieces, []byte("12345678901234567890"))

				files := []map[string]any{
					{
						"length": 1024,
						"path":   []string{"dir", "file1.txt"},
					},
					{
						"length": 2048,
						"path":   []string{"dir", "file2.txt"},
					},
				}

				info := map[string]any{
					"name":         "multi-test",
					"piece length": 32768,
					"pieces":       string(pieces),
					"files":        files,
					"private":      0,
				}

				data := map[string]any{
					"announce":      "http://tracker.example.com/announce",
					"announce-list": [][]string{{"http://backup.tracker.com/announce"}},
					"created by":    "test-suite",
					"creation date": 1234567890,
					"encoding":      "UTF-8",
					"info":          info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			want: func() *metainfo.Metainfo {
				pieces := make([]byte, 20)
				copy(pieces, []byte("12345678901234567890"))

				return &metainfo.Metainfo{
					Announce:     "http://tracker.example.com/announce",
					AnnounceList: [][]string{{"http://backup.tracker.com/announce"}},
					CreatedBy:    "test-suite",
					CreationDate: 1234567890,
					Encoding:     "UTF-8",
					Info: metainfo.InfoDict{
						Name:     "multi-test",
						PieceLen: 32768,
						Pieces:   pieces,
						Files: []metainfo.File{
							{Len: 1024, Path: []string{"dir", "file1.txt"}},
							{Len: 2048, Path: []string{"dir", "file2.txt"}},
						},
						Private: 0,
					},
				}
			},
		},
		{
			name: "minimal valid torrent",
			data: func() []byte {
				pieces := make([]byte, 20)
				copy(pieces, []byte("12345678901234567890"))

				info := map[string]any{
					"name":         "minimal.txt",
					"piece length": 16384,
					"pieces":       string(pieces),
					"length":       1000,
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			want: func() *metainfo.Metainfo {
				pieces := make([]byte, 20)
				copy(pieces, []byte("12345678901234567890"))

				return &metainfo.Metainfo{
					Announce: "http://tracker.example.com/announce",
					Info: metainfo.InfoDict{
						Name:     "minimal.txt",
						PieceLen: 16384,
						Pieces:   pieces,
						Len:      1000,
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := metainfo.ParseMetainfoFromBytes(tt.data())
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMetainfoFromBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ParseMetainfoFromBytes() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			want := tt.want()
			if got.Announce != want.Announce {
				t.Errorf("Announce = %v, want %v", got.Announce, want.Announce)
			}
			if got.Comment != want.Comment {
				t.Errorf("Comment = %v, want %v", got.Comment, want.Comment)
			}
			if got.CreatedBy != want.CreatedBy {
				t.Errorf("CreatedBy = %v, want %v", got.CreatedBy, want.CreatedBy)
			}
			if got.CreationDate != want.CreationDate {
				t.Errorf("CreationDate = %v, want %v", got.CreationDate, want.CreationDate)
			}
			if got.Encoding != want.Encoding {
				t.Errorf("Encoding = %v, want %v", got.Encoding, want.Encoding)
			}
			if !reflect.DeepEqual(got.AnnounceList, want.AnnounceList) {
				t.Errorf("AnnounceList = %v, want %v", got.AnnounceList, want.AnnounceList)
			}

			if got.InfoHash == [20]byte{} {
				t.Error("InfoHash should not be empty")
			}

			if !reflect.DeepEqual(got.Info, want.Info) {
				t.Errorf("Info = %v, want %v", got.Info, want.Info)
			}
		})
	}
}

func TestParseMetainfoFromBytesErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    func() []byte
		wantErr string
	}{
		{
			name: "empty data",
			data: func() []byte {
				return []byte{}
			},
			wantErr: "bencode",
		},
		{
			name: "invalid bencode",
			data: func() []byte {
				return []byte("not bencode data")
			},
			wantErr: "bencode",
		},
		{
			name: "missing announce and announce-list",
			data: func() []byte {
				pieces := make([]byte, 20)
				info := map[string]any{
					"name":         "test.txt",
					"piece length": 32768,
					"pieces":       string(pieces),
					"length":       1000,
				}

				data := map[string]any{
					"info": info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			wantErr: "no announce URL found",
		},
		{
			name: "pieces not multiple of 20",
			data: func() []byte {
				pieces := make([]byte, 19)

				info := map[string]any{
					"name":         "test.txt",
					"piece length": 32768,
					"pieces":       string(pieces),
					"length":       1000,
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			wantErr: "pieces string length not multiple of 20",
		},
		{
			name: "invalid piece length",
			data: func() []byte {
				pieces := make([]byte, 20)

				info := map[string]any{
					"name":         "test.txt",
					"piece length": 0,
					"pieces":       string(pieces),
					"length":       1000,
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			wantErr: "invalid piece length",
		},
		{
			name: "both length and files present",
			data: func() []byte {
				pieces := make([]byte, 20)

				files := []map[string]any{
					{"length": 1024, "path": []string{"file.txt"}},
				}

				info := map[string]any{
					"name":         "test.txt",
					"piece length": 32768,
					"pieces":       string(pieces),
					"length":       1000,
					"files":        files,
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			wantErr: "exactly one of length or files must be present",
		},
		{
			name: "neither length nor files present",
			data: func() []byte {
				pieces := make([]byte, 20)

				info := map[string]any{
					"name":         "test.txt",
					"piece length": 32768,
					"pieces":       string(pieces),
				}

				data := map[string]any{
					"announce": "http://tracker.example.com/announce",
					"info":     info,
				}

				encoded, _ := bencode.Marshal(data)
				return encoded
			},
			wantErr: "exactly one of length or files must be present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := metainfo.ParseMetainfoFromBytes(tt.data())
			if err == nil {
				t.Errorf("ParseMetainfoFromBytes() expected error but got none")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseMetainfoFromBytes() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestInfoHashGeneration(t *testing.T) {
	pieces := make([]byte, 20)
	copy(pieces, []byte("12345678901234567890"))

	info := map[string]any{
		"name":         "test.txt",
		"piece length": 32768,
		"pieces":       string(pieces),
		"length":       1000,
	}

	data := map[string]any{
		"announce": "http://tracker.example.com/announce",
		"info":     info,
	}

	encoded, _ := bencode.Marshal(data)
	mi, err := metainfo.ParseMetainfoFromBytes(encoded)
	if err != nil {
		t.Fatalf("ParseMetainfoFromBytes() error = %v", err)
	}

	if mi.InfoHash == [20]byte{} {
		t.Error("InfoHash should not be empty")
	}

	mi2, err := metainfo.ParseMetainfoFromBytes(encoded)
	if err != nil {
		t.Fatalf("ParseMetainfoFromBytes() second call error = %v", err)
	}

	if mi.InfoHash != mi2.InfoHash {
		t.Error("InfoHash should be deterministic")
	}

	infoBytes, _ := bencode.Marshal(mi.Info)
	expectedHash := sha1.Sum(infoBytes)
	if mi.InfoHash != expectedHash {
		t.Errorf("InfoHash = %x, want %x", mi.InfoHash, expectedHash)
	}
}

func TestComplexTorrentData(t *testing.T) {
	pieces := make([]byte, 60)
	for i := 0; i < 3; i++ {
		hash := sha1.Sum([]byte(strings.Repeat("a", 20)))
		copy(pieces[i*20:(i+1)*20], hash[:])
	}

	files := []map[string]any{
		{
			"length": 10240,
			"path":   []string{"dir1", "subdir1", "file1.txt"},
			"md5sum": "md5hash1",
		},
		{
			"length": 20480,
			"path":   []string{"dir1", "subdir2", "file2.bin"},
		},
		{
			"length": 5120,
			"path":   []string{"dir2", "file3.dat"},
			"md5sum": "md5hash3",
		},
	}

	info := map[string]any{
		"name":         "complex-torrent",
		"piece length": 16384,
		"pieces":       string(pieces),
		"files":        files,
		"private":      1,
	}

	announceList := [][]string{
		{"http://primary.tracker.com/announce"},
		{"http://backup1.tracker.com/announce", "http://backup2.tracker.com/announce"},
		{"udp://udp.tracker.com:1337/announce"},
	}

	data := map[string]any{
		"announce":      "http://primary.tracker.com/announce",
		"announce-list": announceList,
		"comment":       "Complex test torrent with multiple files and trackers",
		"created by":    "metainfo_test",
		"creation date": 1640995200,
		"encoding":      "UTF-8",
		"info":          info,
	}

	encoded, _ := bencode.Marshal(data)
	mi, err := metainfo.ParseMetainfoFromBytes(encoded)
	if err != nil {
		t.Fatalf("ParseMetainfoFromBytes() error = %v", err)
	}

	if mi.Announce != "http://primary.tracker.com/announce" {
		t.Errorf("Announce = %v, want %v", mi.Announce, "http://primary.tracker.com/announce")
	}

	if !reflect.DeepEqual(mi.AnnounceList, announceList) {
		t.Errorf("AnnounceList = %v, want %v", mi.AnnounceList, announceList)
	}

	if mi.Comment != "Complex test torrent with multiple files and trackers" {
		t.Errorf("Comment = %v", mi.Comment)
	}

	if mi.CreatedBy != "metainfo_test" {
		t.Errorf("CreatedBy = %v", mi.CreatedBy)
	}

	if mi.CreationDate != 1640995200 {
		t.Errorf("CreationDate = %v", mi.CreationDate)
	}

	if mi.Encoding != "UTF-8" {
		t.Errorf("Encoding = %v", mi.Encoding)
	}

	if mi.Info.Name != "complex-torrent" {
		t.Errorf("Info.Name = %v", mi.Info.Name)
	}

	if mi.Info.PieceLen != 16384 {
		t.Errorf("Info.PieceLen = %v", mi.Info.PieceLen)
	}

	if len(mi.Info.Pieces) != 60 {
		t.Errorf("len(Info.Pieces) = %v, want 60", len(mi.Info.Pieces))
	}

	if len(mi.Info.Files) != 3 {
		t.Errorf("len(Info.Files) = %v, want 3", len(mi.Info.Files))
	}

	if mi.Info.Private != 1 {
		t.Errorf("Info.Private = %v, want 1", mi.Info.Private)
	}

	if mi.InfoHash == [20]byte{} {
		t.Error("InfoHash should not be empty for complex torrent")
	}
}
