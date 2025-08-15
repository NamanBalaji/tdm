package torrent

import (
	"net/http"
	"strings"

	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
)

func HasTorrentFile(url string) bool {
	if strings.HasSuffix(url, ".torrent") {
		return true
	}

	resp, err := http.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	switch contentType {
	case "application/x-bittorrent",
		"application/torrent":
		return true
	default:
		return false
	}
}

func IsValidMagnetLink(url string) bool {
	_, err := metainfo.ParseMagnet(url)
	if err != nil {
		return false
	}

	return true
}
