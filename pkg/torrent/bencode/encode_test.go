package bencode_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []byte
		wantErr error
	}{
		{"empty-string", "", []byte("0:"), nil},
		{"ascii-string", "spam", []byte("4:spam"), nil},
		{"binary-string", string([]byte{0xff, 0x00, 0x61}), []byte("3:\xff\x00a"), nil},

		{"empty-bytes", []byte{}, []byte("0:"), nil},
		{"bytes-data", []byte("hello"), []byte("5:hello"), nil},
		{"binary-bytes", []byte{0xff, 0x00, 0x61}, []byte("3:\xff\x00a"), nil},

		{"int-zero", int(0), []byte("i0e"), nil},
		{"int-positive", int(42), []byte("i42e"), nil},
		{"int-negative", int(-7), []byte("i-7e"), nil},
		{"int64-max", int64(1234567890), []byte("i1234567890e"), nil},
		{"uint64-max", uint64(9876543210), []byte("i9876543210e"), nil},
		{"int8-min", int8(-128), []byte("i-128e"), nil},
		{"uint16-val", uint16(65535), []byte("i65535e"), nil},

		{"empty-list", []any{}, []byte("le"), nil},
		{"list-mixed", []any{"spam", int(1), []byte("foo")},
			[]byte("l4:spami1e3:fooe"), nil},

		{"empty-dict", map[string]any{}, []byte("de"), nil},
		{"dict-simple", map[string]any{"bar": "foo", "baz": int(2)},
			[]byte("d3:bar3:foo3:bazi2ee"), nil},
		{"dict-nested", map[string]any{"z": []any{1, "a"}, "a": map[string]any{"x": []byte{'y'}}},
			[]byte("d1:ad1:x1:ye1:zli1e1:aee"), nil},

		{"bool-type", true, nil, bencode.ErrEncoding},
		{"float-type", 3.14, nil, bencode.ErrEncoding},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := bencode.Encode(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Encode(%#v) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if !bytes.Equal(out, tc.want) {
					t.Errorf("Encode(%#v)\nGot:  %q (%v)\nWant: %q (%v)", tc.input, out, out, tc.want, tc.want)
				}
			}
		})
	}
}

func TestRoundTripBinary(t *testing.T) {
	testCases := []struct {
		name     string
		input    any
		expected any
	}{
		{
			"binary-data",
			[]byte{0x00, 0x01, 0x02, 0xff, 0xfe},
			[]byte{0x00, 0x01, 0x02, 0xff, 0xfe},
		},
		{
			"string-becomes-bytes",
			"hello world",
			[]byte("hello world"),
		},
		{
			"mixed-data",
			[]byte("mixed\x00\xff\x01data"),
			[]byte("mixed\x00\xff\x01data"),
		},
		{
			"dict-with-string-values",
			map[string]any{
				"pieces": []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc},
				"name":   "test.txt",
			},
			map[string]any{
				"pieces": []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc},
				"name":   []byte("test.txt"),
			},
		},
		{
			"nested-structures",
			[]any{"spam", int(1), []byte("foo")},
			[]any{[]byte("spam"), int64(1), []byte("foo")},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := bencode.Encode(tc.input)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			decoded, _, err := bencode.Decode(encoded)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if !reflect.DeepEqual(decoded, tc.expected) {
				t.Fatalf("Roundtrip failed:\nInput:    %#v\nDecoded:  %#v\nExpected: %#v", tc.input, decoded, tc.expected)
			}
		})
	}
}
