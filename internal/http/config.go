package http

import (
	"time"
)

type ConfigOption func(*Config)

type Config struct {
	Connections int               `json:"connections"`
	MaxChunks   int               `json:"maxChunks"`
	Headers     map[string]string `json:"headers,omitempty"`
	MaxRetries  int               `json:"maxRetries"`
	RetryDelay  time.Duration     `json:"retryDelay,omitempty"`
}

func defaultConfig() *Config {
	return &Config{
		Connections: 8,
		Headers:     make(map[string]string),
		MaxRetries:  3,
		RetryDelay:  2 * time.Second,
		MaxChunks:   32,
	}
}

func WithMaxChunks(maxChunks int) ConfigOption {
	return func(cfg *Config) {
		if maxChunks <= 0 {
			maxChunks = 32
		}

		cfg.MaxChunks = maxChunks
	}
}

func WithConnections(connections int) ConfigOption {
	return func(cfg *Config) {
		cfg.Connections = connections
	}
}

func WithHeaders(headers map[string]string) ConfigOption {
	return func(cfg *Config) {
		cfg.Headers = headers
	}
}

func WithMaxRetries(maxRetries int) ConfigOption {
	return func(cfg *Config) {
		cfg.MaxRetries = maxRetries
	}
}

func WithRetryDelay(retryDelay time.Duration) ConfigOption {
	return func(cfg *Config) {
		cfg.RetryDelay = retryDelay
	}
}
