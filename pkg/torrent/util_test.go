package torrent_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent"
)

func newCTServer(t *testing.T, ct string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure header is set before status line is written.
		w.Header().Set("Content-Type", ct)
		// HEAD should not write a body, but headers + status must be present.
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("ok"))
		}
	}))
}

func TestHasTorrentFile(t *testing.T) {
	t.Run("suffix .torrent short-circuits to true", func(t *testing.T) {
		got := torrent.HasTorrentFile("http://example.com/file.torrent")
		if !got {
			t.Fatalf("want true")
		}
	})

	t.Run("HEAD Content-Type application/x-bittorrent", func(t *testing.T) {
		ts := newCTServer(t, "application/x-bittorrent")
		defer ts.Close()
		got := torrent.HasTorrentFile(ts.URL + "/bittorrent")
		if !got {
			t.Fatalf("want true")
		}
	})

	t.Run("HEAD Content-Type application/torrent", func(t *testing.T) {
		ts := newCTServer(t, "application/torrent")
		defer ts.Close()
		got := torrent.HasTorrentFile(ts.URL + "/alt")
		if !got {
			t.Fatalf("want true")
		}
	})

	t.Run("HEAD Content-Type not a torrent", func(t *testing.T) {
		ts := newCTServer(t, "text/html")
		defer ts.Close()
		got := torrent.HasTorrentFile(ts.URL + "/html")
		if got {
			t.Fatalf("want false")
		}
	})

	t.Run("HEAD error returns false", func(t *testing.T) {
		got := torrent.HasTorrentFile("http://[::1]:-1")
		if got {
			t.Fatalf("want false")
		}
	})
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := torrent.IsValidMagnetLink(tc.in); got != tc.want {
				t.Fatalf("IsValidMagnetLink(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
