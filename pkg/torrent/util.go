package torrent

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/NamanBalaji/tdm/internal/logger"
)

// HasTorrentFile checks if a given URL points to a torrent file by examining its extension and Content-Type header.
func HasTorrentFile(url string) bool {
	if strings.HasSuffix(url, ".torrent") {
		return true
	}

	resp, err := http.Head(url)
	if err != nil {
		return false
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Warnf("Failed to close response body: %v", err)
		}
	}()

	contentType := resp.Header.Get("Content-Type")
	switch contentType {
	case "application/x-bittorrent",
		"application/torrent":
		return true
	default:
		return false
	}
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
