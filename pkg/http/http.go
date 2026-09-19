package http

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultIdleTimeout    = 90 * time.Second
	keepAlivePeriod       = 30 * time.Second
	maxIdleConns          = 100
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
	maxConnsPerHost       = 16

	DefaultUserAgent = "TDM/1.0"

	defaultDownloadName = "download"
)

type Client struct {
	*http.Client
}

// NewClient creates a new HTTP client with custom transport settings.
// perHostConns raises the per-host connection limit above the default of
// 16 so a configured connection count is never silently capped.
func NewClient(perHostConns int) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultConnectTimeout,
			KeepAlive: keepAlivePeriod,
		}).DialContext,
		MaxIdleConns:          maxIdleConns,
		IdleConnTimeout:       defaultIdleTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		DisableCompression:    true,
		MaxConnsPerHost:       max(perHostConns, maxConnsPerHost),
	}

	return &Client{
		&http.Client{
			Transport: transport,
		},
	}
}

// IsDownloadable reports whether the URL is a candidate for HTTP download.
// It is purely syntactic — no network I/O. Whether the content is actually
// downloadable is determined by the probe at Init time.
func IsDownloadable(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Head performs a HEAD request to the specified URL with optional headers.
func (c *Client) Head(ctx context.Context, urlStr string, headers map[string]string) (*http.Response, error) {
	slog.Debug("initializing with HEAD request", "url", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodHead, headers)
	if err != nil {
		slog.Error("failed to create HEAD request", "url", urlStr, "err", err)
		return nil, err
	}

	slog.Debug("sending HEAD request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("HEAD request failed", "url", urlStr, "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("HEAD response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("HEAD request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// Range performs a Range GET request to the specified URL. It takes in the start byte and the end byte.
func (c *Client) Range(ctx context.Context, urlStr string, start, end int64, headers map[string]string) (*http.Response, error) {
	slog.Debug("initializing with Range GET request", "url", urlStr)

	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := generateRequest(ctx, urlStr, http.MethodGet, headers)
	if err != nil {
		slog.Error("failed to create Range GET request", "url", urlStr, "err", err)
		return nil, err
	}

	rangeVal := fmt.Sprintf("bytes=%d-%d", start, end)
	req.Header.Set("Range", rangeVal)
	slog.Debug("set Range header", "range", rangeVal, "url", urlStr)

	slog.Debug("sending Range GET request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("Range GET request failed", "url", urlStr, "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("Range GET response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("Range GET request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	if resp.StatusCode != http.StatusPartialContent {
		slog.Warn("server doesn't support ranges", "url", urlStr, "status", resp.StatusCode)
		return nil, ErrRangesNotSupported
	}

	return resp, nil
}

// Get performs a GET request to the specified URL.
func (c *Client) Get(ctx context.Context, urlStr string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		slog.Error("failed to create fallback GET request", "err", err)
		return nil, ErrRequestCreation
	}

	slog.Debug("sending fallback GET request", "url", urlStr)

	resp, err := c.Do(req)
	if err != nil {
		slog.Error("fallback GET request failed", "err", err)
		return nil, ClassifyError(err)
	}

	slog.Debug("closing body immediately for fallback GET request")

	slog.Debug("GET response", "url", urlStr, "status", resp.StatusCode)

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("GET request returned error status", "status", resp.StatusCode, "url", urlStr)
		return nil, ClassifyHTTPError(resp.StatusCode)
	}

	return resp, nil
}

// generateRequest creates a new HTTP request with the specified method and URL.
func generateRequest(ctx context.Context, urlStr, method string, headers map[string]string) (*http.Request, error) {
	slog.Debug("creating request", "method", method, "url", urlStr)

	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		slog.Error("failed to create request", "method", method, "url", urlStr, "err", err)
		return nil, ErrRequestCreation
	}

	req.Header.Set("User-Agent", DefaultUserAgent)

	for key, value := range headers {
		req.Header.Set(key, value)
		slog.Debug("set custom header", "key", key)
	}

	return req, nil
}

// GetFilename tries extracts the filename from the Content-Disposition header or the URL.
// All sources are server-controlled, so every candidate is sanitized before use.
func GetFilename(resp *http.Response) string {
	if fileName, ok := getFileNameFromContentDisposition(resp.Header.Get("Content-Disposition")); ok {
		if s := SanitizeFilename(fileName); s != "" {
			return s
		}
	}

	u := resp.Request.URL
	if qname := u.Query().Get("filename"); qname != "" {
		if s := SanitizeFilename(qname); s != "" {
			return s
		}
	}

	if s := SanitizeFilename(path.Base(u.Path)); s != "" {
		return s
	}

	return defaultDownloadName
}

// SanitizeFilename reduces an untrusted filename to a single safe path
// element: no directory components, control characters, or Windows
// reserved names. Returns "" if nothing safe remains.
func SanitizeFilename(name string) string {
	// A hostile server can smuggle traversal like "../../etc/cron.d/x"
	// (with either separator); keep only the last path element.
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)

	if name == "/" || name == "." || name == ".." {
		return ""
	}

	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		switch r {
		case '<', '>', ':', '"', '|', '?', '*':
			return '_'
		}

		return r
	}, name)

	// Windows strips trailing dots and spaces, which could turn "safe.."
	// into an unexpected name.
	name = strings.TrimRight(name, " .")
	if name == "" {
		return ""
	}

	name = truncateFilename(name)

	if isWindowsReservedName(name) {
		name = "_" + name
	}

	return name
}

// maxFilenameBytes caps sanitized names well under the common 255-byte
// filesystem limit, leaving room for the suffixes tdm appends (".<rand>.part"
// merge temps, ".N" collision renames).
const maxFilenameBytes = 200

// truncateFilename shortens over-long names, preserving the extension and
// never splitting a multi-byte rune.
func truncateFilename(name string) string {
	if len(name) <= maxFilenameBytes {
		return name
	}

	ext := path.Ext(name)
	if len(ext) > maxFilenameBytes/2 {
		ext = ""
	}

	base := name[:maxFilenameBytes-len(ext)]
	for len(base) > 0 && !utf8.ValidString(base) {
		base = base[:len(base)-1]
	}

	return strings.TrimRight(base, " .") + ext
}

// isWindowsReservedName reports whether name (ignoring any extension) is a
// reserved device name on Windows, e.g. "CON" or "com1.txt".
func isWindowsReservedName(name string) bool {
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}

	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
		base[3] >= '1' && base[3] <= '9' {
		return true
	}

	return false
}

func getFileNameFromContentDisposition(header string) (string, bool) {
	if header == "" {
		return "", false
	}

	if _, params, err := mime.ParseMediaType(header); err == nil {
		if fName := cmp.Or(params["filename"], params["filename*"]); fName != "" {
			return fName, true
		}
	}

	return "", false
}

// ParseLastModified parses the Last-Modified header.
func ParseLastModified(header string) time.Time {
	if header == "" {
		return time.Time{}
	}

	// Try to parse the header (RFC1123 format)
	t, err := time.Parse(time.RFC1123, header)
	if err != nil {
		slog.Debug("failed to parse Last-Modified header", "header", header, "err", err)
		return time.Time{}
	}

	slog.Debug("parsed Last-Modified", "time", t)

	return t
}
