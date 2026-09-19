package torrent_test

import (
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent"
)

func TestHasTorrentFile(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"suffix .torrent is true", "http://example.com/file.torrent", true},
		{".torrent path with query string", "http://example.com/file.torrent?token=abc", true},
		{"no .torrent suffix is false (no network probe)", "http://example.com/file.iso", false},
		{"URL ending in .torrent via the query (as in v0.4.0)", "http://example.com/dl?file=x.torrent", true},
		{".torrent mid-query is false", "http://example.com/dl?file=x.torrent&t=1", false},
		{"tracker link without .torrent is false (probe hands it off)", "http://example.com/download.php?id=123", false},
		{"bare filename", "file.torrent", true},
		{"html page", "http://example.com/index.html", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := torrent.HasTorrentFile(tc.in); got != tc.want {
				t.Fatalf("HasTorrentFile(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsValidMagnetLink(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"valid magnet with xt btih", "magnet:?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12", true},
		{"valid with extra params", "magnet:?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12&dn=Some+Name", true},
		{"missing magnet prefix", "magnet:xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12", false},
		{"bad URL encoding", "magnet:?%zz", false},
		{"wrong scheme", "http://example.com?xt=urn:btih:abcdef1234567890abcdef1234567890abcdef12", false},
		{"missing xt", "magnet:?dn=OnlyName", false},
		{"xt wrong urn", "magnet:?xt=urn:btmh:somehash", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := torrent.IsValidMagnetLink(tc.in); got != tc.want {
				t.Fatalf("IsValidMagnetLink(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
