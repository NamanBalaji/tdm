package torrent

import (
	"net/url"
	"strings"
)

// HasTorrentFile reports whether a URL points to a torrent file, judged from
// the URL alone (no network I/O): its path, or the whole URL, ends in
// ".torrent". Links that serve a torrent without saying so in the URL
// (e.g. download.php?id=123) are caught later, when the HTTP downloader's
// probe sees the torrent Content-Type and hands the download over.
func HasTorrentFile(rawURL string) bool {
	if strings.HasSuffix(rawURL, ".torrent") {
		return true
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	return strings.HasSuffix(u.Path, ".torrent")
}

// IsValidMagnetLink checks if a given string is a valid magnet link.
func IsValidMagnetLink(urlStr string) bool {
	if !strings.HasPrefix(urlStr, "magnet:?") {
		return false
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	if u.Scheme != "magnet" {
		return false
	}

	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}

	xt := params.Get("xt")
	if xt == "" {
		return false
	}

	return strings.HasPrefix(xt, "urn:btih:")
}
