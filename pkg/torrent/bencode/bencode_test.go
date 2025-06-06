package bencode_test

import (
	"bytes"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent/bencode"
)

func TestEncodeDecodeInt(t *testing.T) {
	tests := []struct {
		name    string
		value   int64
		encoded string
	}{
		{"zero", 0, "i0e"},
		{"positive", 42, "i42e"},
		{"negative", -42, "i-42e"},
		{"large", 1234567890, "i1234567890e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded, err := bencode.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(encoded) != tt.encoded {
				t.Errorf("Expected encoded %q, got %q", tt.encoded, string(encoded))
			}

			// Test decoding
			var decoded int64
			err = bencode.Unmarshal(encoded, &decoded)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded != tt.value {
				t.Errorf("Expected decoded %d, got %d", tt.value, decoded)
			}
		})
	}
}

func TestEncodeDecodeString(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		encoded string
	}{
		{"empty", "", "0:"},
		{"simple", "hello", "5:hello"},
		{"with spaces", "hello world", "11:hello world"},
		{"unicode", "🚀", "4:🚀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded, err := bencode.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(encoded) != tt.encoded {
				t.Errorf("Expected encoded %q, got %q", tt.encoded, string(encoded))
			}

			// Test decoding
			var decoded string
			err = bencode.Unmarshal(encoded, &decoded)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded != tt.value {
				t.Errorf("Expected decoded %q, got %q", tt.value, decoded)
			}
		})
	}
}

func TestEncodeDecodeBytes(t *testing.T) {
	tests := []struct {
		name    string
		value   []byte
		encoded string
	}{
		{"empty", []byte{}, "0:"},
		{"simple", []byte("hello"), "5:hello"},
		{"binary", []byte{0, 1, 2, 3}, "4:\x00\x01\x02\x03"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded, err := bencode.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(encoded) != tt.encoded {
				t.Errorf("Expected encoded %q, got %q", tt.encoded, string(encoded))
			}

			// Test decoding
			var decoded []byte
			err = bencode.Unmarshal(encoded, &decoded)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if !bytes.Equal(decoded, tt.value) {
				t.Errorf("Expected decoded %v, got %v", tt.value, decoded)
			}
		})
	}
}

func TestEncodeDecodeList(t *testing.T) {
	tests := []struct {
		name    string
		value   []interface{}
		encoded string
	}{
		{"empty", []interface{}{}, "le"},
		{"integers", []interface{}{int64(1), int64(2), int64(3)}, "li1ei2ei3ee"},
		{"strings", []interface{}{"a", "b"}, "l1:a1:be"},
		{"mixed", []interface{}{int64(42), "hello"}, "li42e5:helloe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded, err := bencode.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(encoded) != tt.encoded {
				t.Errorf("Expected encoded %q, got %q", tt.encoded, string(encoded))
			}

			// Test decoding
			var decoded []interface{}
			err = bencode.Unmarshal(encoded, &decoded)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if len(decoded) != len(tt.value) {
				t.Errorf("Expected %d items, got %d", len(tt.value), len(decoded))
			}
		})
	}
}

func TestEncodeDecodeDict(t *testing.T) {
	tests := []struct {
		name    string
		value   map[string]interface{}
		encoded string
	}{
		{"empty", map[string]interface{}{}, "de"},
		{"single", map[string]interface{}{"key": "value"}, "d3:key5:valuee"},
		{"multiple", map[string]interface{}{"a": int64(1), "b": int64(2)}, "d1:ai1e1:bi2ee"},
		{"sorted", map[string]interface{}{"z": "last", "a": "first"}, "d1:a5:first1:z4:laste"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encoding
			encoded, err := bencode.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(encoded) != tt.encoded {
				t.Errorf("Expected encoded %q, got %q", tt.encoded, string(encoded))
			}

			// Test decoding
			var decoded map[string]interface{}
			err = bencode.Unmarshal(encoded, &decoded)
			if err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if len(decoded) != len(tt.value) {
				t.Errorf("Expected %d items, got %d", len(tt.value), len(decoded))
			}
		})
	}
}

func TestEncodeDecodeStruct(t *testing.T) {
	type TestStruct struct {
		Name   string `bencode:"name"`
		Age    int    `bencode:"age"`
		Height int    `bencode:"height,omitempty"`
		Ignore string `bencode:"-"`
	}

	original := TestStruct{
		Name:   "Alice",
		Age:    30,
		Height: 0, // Should be omitted
		Ignore: "ignored",
	}

	// Encode
	encoded, err := bencode.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	expected := "d3:agei30e4:name5:Alicee"
	if string(encoded) != expected {
		t.Errorf("Expected encoded %q, got %q", expected, string(encoded))
	}

	// Decode
	var decoded TestStruct
	err = bencode.Unmarshal(encoded, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Expected name %q, got %q", original.Name, decoded.Name)
	}

	if decoded.Age != original.Age {
		t.Errorf("Expected age %d, got %d", original.Age, decoded.Age)
	}

	if decoded.Height != 0 {
		t.Errorf("Expected height 0, got %d", decoded.Height)
	}
}

func TestDecodeInvalidBencode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"invalid integer", "i123", true},
		{"invalid integer with leading zero", "i01e", true},
		{"negative zero", "i-0e", true},
		{"invalid string length", "5:hi", true},
		{"negative string length", "-5:hello", true},
		{"invalid list", "li1e", true},
		{"invalid dict", "d3:key", true},
		{"unsorted dict keys", "d1:bi1e1:ai2ee", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v interface{}
			err := bencode.Unmarshal([]byte(tt.input), &v)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestEncodeDecodeNested(t *testing.T) {
	// Test a complex nested structure like a torrent file
	torrent := map[string]interface{}{
		"announce": "http://tracker.example.com",
		"info": map[string]interface{}{
			"name":         "test.txt",
			"piece length": int64(16384),
			"length":       int64(1024),
			"pieces":       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		},
		"announce-list": []interface{}{
			[]interface{}{"http://tracker1.com", "http://tracker2.com"},
			[]interface{}{"udp://tracker3.com"},
		},
	}

	// Encode
	encoded, err := bencode.Marshal(torrent)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Decode
	var decoded map[string]interface{}
	err = bencode.Unmarshal(encoded, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify structure
	announce, ok := decoded["announce"].([]byte)
	if !ok || string(announce) != "http://tracker.example.com" {
		t.Errorf("Expected announce %q, got %v", "http://tracker.example.com", decoded["announce"])
	}

	info, ok := decoded["info"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected info to be a map")
	}

	name, ok := info["name"].([]byte)
	if !ok || string(name) != "test.txt" {
		t.Errorf("Expected name %q, got %v", "test.txt", info["name"])
	}

	pieceLength, ok := info["piece length"].(int64)
	if !ok || pieceLength != 16384 {
		t.Errorf("Expected piece length 16384, got %v", info["piece length"])
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that encoding and then decoding produces the same result
	original := map[string]interface{}{
		"string": "hello world",
		"int":    int64(42),
		"list":   []interface{}{int64(1), int64(2), int64(3)},
		"dict": map[string]interface{}{
			"nested": "value",
			"number": int64(100),
		},
		"bytes": []byte{0, 1, 2, 3, 4},
	}

	// Encode
	encoded, err := bencode.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Decode
	var decoded map[string]interface{}
	err = bencode.Unmarshal(encoded, &decoded)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Re-encode
	reencoded, err := bencode.Marshal(decoded)
	if err != nil {
		t.Fatalf("Re-marshal failed: %v", err)
	}

	// Should be identical
	if !bytes.Equal(encoded, reencoded) {
		t.Errorf("Round trip failed\nOriginal: %q\nRe-encoded: %q", encoded, reencoded)
	}
}

func TestEncodeUnsupportedType(t *testing.T) {
	// Test encoding unsupported types
	type unsupported struct {
		Ch chan int
	}

	u := unsupported{Ch: make(chan int)}

	_, err := bencode.Marshal(u)
	if err == nil {
		t.Error("Expected error encoding unsupported type, got none")
	}
}

func TestDecodeToWrongType(t *testing.T) {
	// Try to decode an integer into a string
	encoded := []byte("i42e")

	var s string
	err := bencode.Unmarshal(encoded, &s)
	if err == nil {
		t.Error("Expected error decoding integer to string, got none")
	}

	// Try to decode a string into an int
	encoded = []byte("5:hello")

	var i int
	err = bencode.Unmarshal(encoded, &i)
	if err == nil {
		t.Error("Expected error decoding string to int, got none")
	}
}
