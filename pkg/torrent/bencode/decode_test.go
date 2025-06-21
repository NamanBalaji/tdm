package bencode_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVal any
		wantN   int
		wantErr error
	}{
		{"str-zero", "0:", "", 2, nil},
		{"str-one-digit", "4:spam", "spam", 6, nil},
		{"str-multi-digit", "11:hello world", "hello world", 14, nil},
		{"str-leading-zero", "01:a", nil, 0, bencode.ErrInvalidBencode},
		{"str-no-colon", "3abc", nil, 0, bencode.ErrInvalidBencode},
		{"str-short", "5:abcd", nil, 0, bencode.ErrInvalidBencode},

		{"int-pos", "i42e", 42, 4, nil},
		{"int-zero", "i0e", 0, 3, nil},
		{"int-neg", "i-7e", -7, 4, nil},
		{"int-unterminated", "i42", nil, 0, bencode.ErrInvalidBencode},
		{"int-leading-zero", "i042e", nil, 0, bencode.ErrInvalidBencode},
		{"int-neg-zero", "i-0e", nil, 0, bencode.ErrInvalidBencode},

		{"list-simple", "l4:spam4:eggse", []any{"spam", "eggs"}, 14, nil},
		{"list-nested", "ll4:spami1eee", []any{[]any{"spam", 1}}, 13, nil},
		{"list-long-string", "l11:hello worldi2ee", []any{"hello world", 2}, 19, nil},
		{"list-unterminated", "l4:spam", nil, 0, bencode.ErrInvalidBencode},
		{"list-bad-elem", "li042ee", nil, 0, bencode.ErrInvalidBencode},

		{"dict-simple", "d3:bar3:fooe", map[string]any{"bar": "foo"}, 12, nil},
		{"dict-two-types", "d3:bar3:foo3:bazi8ee", map[string]any{"bar": "foo", "baz": 8}, 20, nil},
		{"dict-non-string-key", "di1ei2ee", nil, 0, bencode.ErrInvalidBencode},
		{"dict-unsorted-keys", "d3:foo1:13:bar1:2e", map[string]any{"bar": "2", "foo": "1"}, 18, nil},
		{"dict-dup-key", "d3:foo1:13:foo1:2e", nil, 0, bencode.ErrInvalidBencode},
		{"dict-unterminated", "d3:foo1:1", nil, 0, bencode.ErrInvalidBencode},

		{
			"torrent-single-file",
			"d8:announce29:udp://tracker.example.com:8044:infod6:lengthi1024e4:name8:file.txt12:piece lengthi512e6:pieces20:12345678901234567890ee",
			map[string]any{
				"announce": "udp://tracker.example.com:804",
				"info": map[string]any{
					"length":       1024,
					"name":         "file.txt",
					"piece length": 512,
					"pieces":       "12345678901234567890",
				},
			},
			133,
			nil,
		},

		{
			"torrent-multi-file",
			"d8:announce29:udp://tracker.example.com:8044:infod5:filesld6:lengthi1024e4:pathl9:file1.txteed6:lengthi2048e4:pathl9:file2.txteee4:name8:test_dir12:piece lengthi512e6:pieces20:abcdefghijklmnopqrchee",
			map[string]any{
				"announce": "udp://tracker.example.com:804",
				"info": map[string]any{
					"files": []any{
						map[string]any{
							"length": 1024,
							"path":   []any{"file1.txt"},
						},
						map[string]any{
							"length": 2048,
							"path":   []any{"file2.txt"},
						},
					},
					"name":         "test_dir",
					"piece length": 512,
					"pieces":       "abcdefghijklmnopqrch",
				},
			},
			198,
			nil,
		},

		{"unknown-token", "x", nil, 0, bencode.ErrInvalidBencode},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotVal, gotN, gotErr := bencode.Decode(tc.input)

			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("error: got %v, want %v", gotErr, tc.wantErr)
			}
			if tc.wantErr == nil {
				if gotN != tc.wantN {
					t.Fatalf("index: got %d, want %d", gotN, tc.wantN)
				}
				if !reflect.DeepEqual(gotVal, tc.wantVal) {
					t.Fatalf("value: got %#v, want %#v", gotVal, tc.wantVal)
				}
			}
		})
	}
}
