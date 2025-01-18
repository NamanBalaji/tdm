package http

import (
	"crypto/tls"
	"net/url"
	"time"
)

type ClientConfig struct {
	// Connection settings
	ProxyURL              *url.URL
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int // Add this to limit connections per host
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	MaxRedirects          int

	// Timeouts
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	KeepAliveTimeout      time.Duration // Add this for keep-alive connections

	// TLS
	SkipTLSVerify bool
	TLSConfig     *tls.Config

	// Headers
	DefaultHeaders map[string]string
}

// DefaultConfig returns a ClientConfig with sensible defaults
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxRedirects:          10,
		DialTimeout:           30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		KeepAliveTimeout:      30 * time.Second,

		DefaultHeaders: map[string]string{
			"User-Agent": "TDM/1.0",
		},
	}
}
