package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

var ErrInvalidConfig = errors.New("invalid config")

const configFileName = "tdm"

// flagConfig stores the parsed values from the cli flags.
type flagConfig struct {
	urls                   *string
	maxConcurrentDownloads *int
	tempDir                *string
	connections            *int
	chunks                 *int
	maxRetries             *int
	downloadDir            *string
	seedDisable            *bool
	dhtDisable             *bool
	pexDisable             *bool
}

// Config holds the configuration options for the application.
type Config struct {
	Urls                   []string
	MaxConcurrentDownloads int            `yaml:"maxConcurrentDownloads,omitempty"`
	HTTP                   *HTTPConfig    `yaml:"http,omitempty"`
	Torrent                *TorrentConfig `yaml:"torrent,omitempty"`
}

// HTTPConfig holds configuration options for HTTP downloads.
type HTTPConfig struct {
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

func (h *HTTPConfig) IsConfig() bool {
	return true
}

// GetConfig reads the configuration file and returns a Config struct.
// If the configuration file does not exist, it uses default configuration
// but STILL applies CLI flags.
func GetConfig() (*Config, error) {
	configFilePath := filepath.Join(xdg.ConfigHome, configFileName)
	defaults := DefaultConfig()

	var cfg Config // Empty by default

	b, err := os.ReadFile(configFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(b) > 0 {
		err = yaml.Unmarshal(b, &cfg)
		if err != nil {
			return nil, err
		}
	}

	httpCfg := zeroOr(cfg.HTTP, defaults.HTTP)
	torrentCfg := zeroOr(cfg.Torrent, defaults.Torrent)

	conf := Config{
		MaxConcurrentDownloads: zeroOr(cfg.MaxConcurrentDownloads, defaults.MaxConcurrentDownloads),
		HTTP: &HTTPConfig{
			TempDir:     zeroOr(httpCfg.TempDir, defaults.HTTP.TempDir),
			DownloadDir: zeroOr(httpCfg.DownloadDir, defaults.HTTP.DownloadDir),
			Connections: zeroOr(httpCfg.Connections, defaults.HTTP.Connections),
			Chunks:      zeroOr(httpCfg.Chunks, defaults.HTTP.Chunks),
			MaxRetries:  zeroOr(httpCfg.MaxRetries, defaults.HTTP.MaxRetries),
			RetryDelay:  zeroOr(httpCfg.RetryDelay, defaults.HTTP.RetryDelay),
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
	}

	conf.applyFlagsToConfig()

	if err := conf.validate(); err != nil {
		return nil, err
	}

	return &conf, nil
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

// applyFlagsToConfig takes the value of the cli flags applied at the start and plugs them into the config.
func (c *Config) applyFlagsToConfig() {
	fc := flagConfig{
		urls:                   flag.String("urls", "", "path to the file continoing urls separated by space"),
		maxConcurrentDownloads: flag.Int("mcd", c.MaxConcurrentDownloads, "max number of downloads that run together"),
		tempDir:                flag.String("td", c.HTTP.TempDir, "path to temporary directory for storing HTTP hunks"),
		connections:            flag.Int("conn", c.HTTP.Connections, "number of parallel connections the will be used to download from the server at a time"),
		chunks:                 flag.Int("c", c.HTTP.Chunks, "number to chunks a HTTP download will be split into"),
		maxRetries:             flag.Int("mr", c.HTTP.MaxRetries, "maximum number of retries before the chunk fails"),
		downloadDir:            flag.String("dd", c.HTTP.DownloadDir, "path to the directory that will be used to store new downloads"),
		seedDisable:            flag.Bool("ns", c.Torrent.Seed, "no seed disable seeding for torrents"),
		pexDisable:             flag.Bool("np", c.Torrent.DisablePEX, "no pex will disable peer exchange for torrents"),
		dhtDisable:             flag.Bool("nd", c.Torrent.DisableDHT, "no DHT will diabale DHT for torrents"),
	}

	flag.Parse()

	if fc.urls != nil {
		c.Urls = strings.Split(*fc.urls, " ")
	}

	c.MaxConcurrentDownloads = *fc.maxConcurrentDownloads
	c.HTTP.TempDir = *fc.tempDir
	c.HTTP.Connections = *fc.connections
	c.HTTP.Chunks = *fc.chunks
	c.HTTP.MaxRetries = *fc.maxRetries
	c.HTTP.DownloadDir, c.Torrent.DownloadDir = *fc.downloadDir, *fc.downloadDir
	c.Torrent.Seed = *fc.seedDisable
	c.Torrent.DisablePEX = *fc.pexDisable
	c.Torrent.DisableDHT = *fc.dhtDisable
}

func (c *Config) validate() error {
	if c.MaxConcurrentDownloads <= 0 {
		return ErrInvalidConfig
	}

	if err := c.HTTP.validate(); err != nil {
		return err
	}

	return c.Torrent.validate()
}

func (h *HTTPConfig) validate() error {
	if h.DownloadDir == "" || h.MaxRetries < 0 || h.Chunks <= 0 || h.Connections <= 0 || h.TempDir == "" {
		return ErrInvalidConfig
	}

	return nil
}

func (t *TorrentConfig) validate() error {
	if t.DownloadDir == "" || t.EstablishedConnectionsPerTorrent <= 0 || t.TotalHalfOpenConnections <= 0 || t.HalfOpenConnectionsPerTorrent <= 0 {
		return ErrInvalidConfig
	}

	return nil
}
