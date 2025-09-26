package config

import (
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

const configFileName = "tdm"

// Config holds the configuration options for the application.
type Config struct {
	MaxConcurrentDownloads int            `yaml:"maxConcurrentDownloads,omitempty"`
	Http                   *HttpConfig    `yaml:"http,omitempty"`
	Torrent                *TorrentConfig `yaml:"torrent,omitempty"`
}

// HttpConfig holds configuration options for HTTP downloads.
type HttpConfig struct {
	DownloadDir string        `yaml:"dir,omitempty"`
	TempDir     string        `yaml:"tempDir,omitempty"`
	Connections int           `yaml:"connections,omitempty"`
	Chunks      int           `yaml:"maxChunks,omitempty"`
	MaxRetries  int           `yaml:"maxRetries,omitempty"`
	RetryDelay  time.Duration `yaml:"retryDelay,omitempty,omitempty"`
}

// TorrentConfig holds configuration options for torrent downloads.
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

func (t *TorrentConfig) IsConfig() bool {
	return true
}

func (h *HttpConfig) IsConfig() bool {
	return true
}

// GetConfig reads the configuration file and returns a Config struct.
// If the configuration file does not exist, it returns the default configuration.
func GetConfig() (*Config, error) {
	configFilePath := filepath.Join(xdg.ConfigHome, configFileName)
	defaults := DefaultConfig()

	b, err := os.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &defaults, nil
		}

		return nil, err
	}

	if len(b) == 0 {
		return &defaults, nil
	}

	var cfg Config

	err = yaml.Unmarshal(b, &cfg)
	if err != nil {
		return nil, err
	}

	httpCfg := zeroOr(cfg.Http, defaults.Http)
	torrentCfg := zeroOr(cfg.Torrent, defaults.Torrent)

	return &Config{
		MaxConcurrentDownloads: zeroOr(cfg.MaxConcurrentDownloads, defaults.MaxConcurrentDownloads),
		Http: &HttpConfig{
			TempDir:     zeroOr(httpCfg.TempDir, defaults.Http.TempDir),
			DownloadDir: zeroOr(httpCfg.DownloadDir, defaults.Http.DownloadDir),
			Connections: zeroOr(httpCfg.Connections, defaults.Http.Connections),
			Chunks:      zeroOr(httpCfg.Chunks, defaults.Http.Chunks),
			MaxRetries:  zeroOr(httpCfg.MaxRetries, defaults.Http.MaxRetries),
			RetryDelay:  zeroOr(httpCfg.RetryDelay, defaults.Http.RetryDelay),
		},
		Torrent: &TorrentConfig{
			DownloadDir:                      zeroOr(torrentCfg.DownloadDir, defaults.Torrent.DownloadDir),
			Seed:                             zeroOr(torrentCfg.Seed, defaults.Torrent.Seed),
			EstablishedConnectionsPerTorrent: zeroOr(torrentCfg.EstablishedConnectionsPerTorrent, defaults.Torrent.EstablishedConnectionsPerTorrent),
			HalfOpenConnectionsPerTorrent:    zeroOr(torrentCfg.HalfOpenConnectionsPerTorrent, defaults.Torrent.HalfOpenConnectionsPerTorrent),
			TotalHalfOpenConnections:         zeroOr(torrentCfg.TotalHalfOpenConnections, defaults.Torrent.TotalHalfOpenConnections),
			DisableDHT:                       zeroOr(torrentCfg.DisableDHT, defaults.Torrent.DisableDHT),
			DisablePEX:                       zeroOr(torrentCfg.DisablePEX, defaults.Torrent.DisablePEX),
			DisableTrackers:                  zeroOr(torrentCfg.DisableTrackers, defaults.Torrent.DisableTrackers),
			DisableIPv6:                      zeroOr(torrentCfg.DisableIPv6, defaults.Torrent.DisableIPv6),
			MetainfoTimeout:                  zeroOr(torrentCfg.MetainfoTimeout, defaults.Torrent.MetainfoTimeout),
		},
	}, nil
}

func DefaultConfig() Config {
	return Config{
		MaxConcurrentDownloads: maxConcurrentDownloads,
		Http: &HttpConfig{
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
			DisableDHT:                       disableDHT,
			DisablePEX:                       disablePEX,
			DisableTrackers:                  disableTrackers,
			DisableIPv6:                      disableIPv6,
			MetainfoTimeout:                  metainfoTimeout,
		},
	}
}

// zeroOr returns def if v is the zero value for its type.
func zeroOr[T any](v, def T) T {
	if reflect.ValueOf(v).IsZero() {
		return def
	}

	return v
}
