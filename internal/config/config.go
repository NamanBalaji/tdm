package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

var ErrInvalidConfig = errors.New("invalid config")

const configFileName = "tdm"

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrInvalidConfig
}

type Config struct {
	Urls                   []string
	MaxConcurrentDownloads int            `yaml:"maxConcurrentDownloads,omitempty"`
	HTTP                   *HTTPConfig    `yaml:"http,omitempty"`
	Torrent                *TorrentConfig `yaml:"torrent,omitempty"`
}

type HTTPConfig struct {
	DownloadDir string        `yaml:"dir,omitempty"`
	TempDir     string        `yaml:"tempDir,omitempty"`
	Connections int           `yaml:"connections,omitempty"`
	Chunks      int           `yaml:"maxChunks,omitempty"`
	MaxRetries  int           `yaml:"maxRetries,omitempty"`
	RetryDelay  time.Duration `yaml:"retryDelay,omitempty"`
}

type TorrentConfig struct {
	DownloadDir                      string        `yaml:"dir,omitempty"`
	Seed                             bool          `yaml:"seed,omitempty"`
	EstablishedConnectionsPerTorrent int           `yaml:"establishedConnectionsPerTorrent,omitempty"`
	HalfOpenConnectionsPerTorrent    int           `yaml:"halfOpenConnectionsPerTorrent,omitempty"`
	TotalHalfOpenConnections         int           `yaml:"totalHalfOpenConnections,omitempty"`
	DisableDHT                       bool          `yaml:"disableDht,omitempty"`
	DisablePEX                       bool          `yaml:"disablePex,omitempty"`
	DisableTrackers                  bool          `yaml:"disableTrackers,omitempty"`
	DisableIPv6                      bool          `yaml:"disableIPv6,omitempty"`
	MetainfoTimeout                  time.Duration `yaml:"metainfoTimeout,omitempty"`
}

// GetConfig reads the configuration file, applies defaults, overlays CLI flags,
// and validates the result. It expects flag.Parse() to have already been called.
func GetConfig() (*Config, error) {
	path := filepath.Join(xdg.ConfigHome, configFileName)

	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}

	applyFlags(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadConfigWithFlags loads config from the given path and applies the provided FlagSet.
// Used for testing without touching global flag.CommandLine.
func LoadConfigWithFlags(path string, fs *flag.FlagSet) (*Config, error) {
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}

	applyFlagsFromFlagSet(cfg, fs)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadConfig(path string) (*Config, error) {
	defaults := DefaultConfig()

	var raw rawConfig

	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(b) > 0 {
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return nil, err
		}
	}

	cfg := raw.resolve(defaults)

	return &cfg, nil
}

func DefaultConfig() Config {
	return Config{
		MaxConcurrentDownloads: maxConcurrentDownloads,
		HTTP: &HTTPConfig{
			TempDir:     tempDir,
			DownloadDir: downloadDir,
			Connections: httpConnections,
			Chunks:      httpChunks,
			MaxRetries:  maxRetries,
			RetryDelay:  retryDelay,
		},
		Torrent: &TorrentConfig{
			DownloadDir:                      downloadDir,
			Seed:                             seedTorrent,
			EstablishedConnectionsPerTorrent: establishedConnectionsPerTorrent,
			HalfOpenConnectionsPerTorrent:    halfOpenConnectionsPerTorrent,
			TotalHalfOpenConnections:         totalHalfOpenConnections,
			MetainfoTimeout:                  metainfoTimeout,
		},
	}
}

func (c *Config) validate() error {
	if c.MaxConcurrentDownloads <= 0 {
		return &ValidationError{Field: "maxConcurrentDownloads", Message: "must be positive"}
	}

	if err := c.HTTP.validate(); err != nil {
		return err
	}

	return c.Torrent.validate()
}

func (h *HTTPConfig) validate() error {
	if h.DownloadDir == "" {
		return &ValidationError{Field: "http.dir", Message: "must not be empty"}
	}

	if h.TempDir == "" {
		return &ValidationError{Field: "http.tempDir", Message: "must not be empty"}
	}

	if h.Connections <= 0 {
		return &ValidationError{Field: "http.connections", Message: "must be positive"}
	}

	if h.Chunks <= 0 {
		return &ValidationError{Field: "http.maxChunks", Message: "must be positive"}
	}

	if h.MaxRetries < 0 {
		return &ValidationError{Field: "http.maxRetries", Message: "must not be negative"}
	}

	return nil
}

func (t *TorrentConfig) validate() error {
	if t.DownloadDir == "" {
		return &ValidationError{Field: "torrent.dir", Message: "must not be empty"}
	}

	if t.EstablishedConnectionsPerTorrent <= 0 {
		return &ValidationError{Field: "torrent.establishedConnectionsPerTorrent", Message: "must be positive"}
	}

	if t.HalfOpenConnectionsPerTorrent <= 0 {
		return &ValidationError{Field: "torrent.halfOpenConnectionsPerTorrent", Message: "must be positive"}
	}

	if t.TotalHalfOpenConnections <= 0 {
		return &ValidationError{Field: "torrent.totalHalfOpenConnections", Message: "must be positive"}
	}

	return nil
}
