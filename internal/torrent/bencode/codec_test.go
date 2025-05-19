package bencode_test

import (
	"github.com/NamanBalaji/tdm/internal/torrent/bencode"
	"testing"
)

func TestDecodeString(t *testing.T) {
	testStr := "12:hello world"
	expected := "hello world"
	result := bencode.DecodeString(testStr)
	if result != expected {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}
