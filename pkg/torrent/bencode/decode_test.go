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
		input   []byte
		wantVal any
		wantN   int
		wantErr error
	}{
		{"str-zero", []byte("0:"), []byte(""), 2, nil},
		{"str-one-digit", []byte("4:spam"), []byte("spam"), 6, nil},
		{"str-multi-digit", []byte("11:hello world"), []byte("hello world"), 14, nil},
		{"str-leading-zero", []byte("01:a"), nil, 0, bencode.ErrInvalidBencode},
		{"str-no-colon", []byte("3abc"), nil, 0, bencode.ErrInvalidBencode},
		{"str-short", []byte("5:abcd"), nil, 0, bencode.ErrInvalidBencode},

		{"str-binary", []byte("3:\xff\x00a"), []byte{0xff, 0x00, 0x61}, 5, nil},

		{"int-pos", []byte("i42e"), int64(42), 4, nil},
		{"int-zero", []byte("i0e"), int64(0), 3, nil},
		{"int-neg", []byte("i-7e"), int64(-7), 4, nil},
		{"int-unterminated", []byte("i42"), nil, 0, bencode.ErrInvalidBencode},
		{"int-leading-zero", []byte("i042e"), nil, 0, bencode.ErrInvalidBencode},
		{"int-neg-zero", []byte("i-0e"), nil, 0, bencode.ErrInvalidBencode},

		{"list-simple", []byte("l4:spam4:eggse"), []any{[]byte("spam"), []byte("eggs")}, 14, nil},
		{"list-nested", []byte("ll4:spami1eee"), []any{[]any{[]byte("spam"), int64(1)}}, 13, nil},
		{"list-long-string", []byte("l11:hello worldi2ee"), []any{[]byte("hello world"), int64(2)}, 19, nil},
		{"list-unterminated", []byte("l4:spam"), nil, 0, bencode.ErrInvalidBencode},
		{"list-bad-elem", []byte("li042ee"), nil, 0, bencode.ErrInvalidBencode},

		{"dict-simple", []byte("d3:bar3:fooe"), map[string]any{"bar": []byte("foo")}, 12, nil},
		{"dict-two-types", []byte("d3:bar3:foo3:bazi8ee"), map[string]any{"bar": []byte("foo"), "baz": int64(8)}, 20, nil},
		{"dict-non-string-key", []byte("di1ei2ee"), nil, 0, bencode.ErrInvalidBencode},
		{"dict-unsorted-keys", []byte("d3:foo1:13:bar1:2e"), map[string]any{"bar": []byte("2"), "foo": []byte("1")}, 18, nil},
		{"dict-dup-key", []byte("d3:foo1:13:foo1:2e"), nil, 0, bencode.ErrInvalidBencode},
		{"dict-unterminated", []byte("d3:foo1:1"), nil, 0, bencode.ErrInvalidBencode},

		{
			"torrent-single-file",
			[]byte("d8:announce29:udp://tracker.example.com:8044:infod6:lengthi1024e4:name8:file.txt12:piece lengthi512e6:pieces20:12345678901234567890ee"),
			map[string]any{
				"announce": []byte("udp://tracker.example.com:804"),
				"info": map[string]any{
					"length":       int64(1024),
					"name":         []byte("file.txt"),
					"piece length": int64(512),
					"pieces":       []byte("12345678901234567890"),
				},
			},
			133,
			nil,
		},

		{
			"torrent-multi-file",
			[]byte("d8:announce29:udp://tracker.example.com:8044:infod5:filesld6:lengthi1024e4:pathl9:file1.txteed6:lengthi2048e4:pathl9:file2.txteee4:name8:test_dir12:piece lengthi512e6:pieces20:abcdefghijklmnopqrchee"),
			map[string]any{
				"announce": []byte("udp://tracker.example.com:804"),
				"info": map[string]any{
					"files": []any{
						map[string]any{
							"length": int64(1024),
							"path":   []any{[]byte("file1.txt")},
						},
						map[string]any{
							"length": int64(2048),
							"path":   []any{[]byte("file2.txt")},
						},
					},
					"name":         []byte("test_dir"),
					"piece length": int64(512),
					"pieces":       []byte("abcdefghijklmnopqrch"),
				},
			},
			198,
			nil,
		},

		{"unknown-token", []byte("x"), nil, 0, bencode.ErrInvalidBencode},
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
