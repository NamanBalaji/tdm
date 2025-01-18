package protocol

import "time"

type ProtocolType string

const (
	ProtocolHTTP  ProtocolType = "http"
	ProtocolHTTPS ProtocolType = "https"
	ProtocolFTP   ProtocolType = "ftp"
	ProtocolSFTP  ProtocolType = "sftp"
)

type FileInfo struct {
	Size         int64
	Resumable    bool
	Filename     string
	ContentType  string
	LastModified string
	ETag         string
}

// DownloadOptions provides configuration options for the download process.
// It allows customization of headers, timeouts, retry behavior, and connection limits.
type DownloadOptions struct {
	Headers        map[string]string
	Timeout        time.Duration
	RetryCount     int
	MaxConnections int
}

// Option is a function type that modifies DownloadOptions
type Option func(*DownloadOptions)

// WithHeaders sets custom headers for the download
func WithHeaders(headers map[string]string) Option {
	return func(o *DownloadOptions) {
		if o.Headers == nil {
			o.Headers = make(map[string]string)
		}
		for k, v := range headers {
			o.Headers[k] = v
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(o *DownloadOptions) {
		o.Timeout = timeout
	}
}

func WithRetryCount(count int) Option {
	return func(o *DownloadOptions) {
		o.RetryCount = count
	}
}

func WithMaxConnections(maxConn int) Option {
	return func(o *DownloadOptions) {
		o.MaxConnections = maxConn
	}
}
