package config

import "time"

type rawConfig struct {
	MaxConcurrentDownloads *int              `yaml:"maxConcurrentDownloads,omitempty"`
	HTTP                   *rawHTTPConfig    `yaml:"http,omitempty"`
	Torrent                *rawTorrentConfig `yaml:"torrent,omitempty"`
}

type rawHTTPConfig struct {
	DownloadDir *string        `yaml:"dir,omitempty"`
	TempDir     *string        `yaml:"tempDir,omitempty"`
	Connections *int           `yaml:"connections,omitempty"`
	Chunks      *int           `yaml:"maxChunks,omitempty"`
	MaxRetries  *int           `yaml:"maxRetries,omitempty"`
	RetryDelay  *time.Duration `yaml:"retryDelay,omitempty"`
}

type rawTorrentConfig struct {
	DownloadDir                      *string        `yaml:"dir,omitempty"`
	Seed                             *bool          `yaml:"seed,omitempty"`
	EstablishedConnectionsPerTorrent *int           `yaml:"establishedConnectionsPerTorrent,omitempty"`
	HalfOpenConnectionsPerTorrent    *int           `yaml:"halfOpenConnectionsPerTorrent,omitempty"`
	TotalHalfOpenConnections         *int           `yaml:"totalHalfOpenConnections,omitempty"`
	DisableDHT                       *bool          `yaml:"disableDht,omitempty"`
	DisablePEX                       *bool          `yaml:"disablePex,omitempty"`
	DisableTrackers                  *bool          `yaml:"disableTrackers,omitempty"`
	DisableIPv6                      *bool          `yaml:"disableIPv6,omitempty"`
	MetainfoTimeout                  *time.Duration `yaml:"metainfoTimeout,omitempty"`
}

func valueOr[T any](p *T, def T) T {
	if p != nil {
		return *p
	}

	return def
}

func (r *rawConfig) resolve(defaults Config) Config {
	return Config{
		MaxConcurrentDownloads: valueOr(r.MaxConcurrentDownloads, defaults.MaxConcurrentDownloads),
		HTTP:                   resolveHTTP(r.HTTP, defaults.HTTP),
		Torrent:                resolveTorrent(r.Torrent, defaults.Torrent),
	}
}

func resolveHTTP(raw *rawHTTPConfig, defaults *HTTPConfig) *HTTPConfig {
	if raw == nil {
		return defaults
	}

	return &HTTPConfig{
		DownloadDir: valueOr(raw.DownloadDir, defaults.DownloadDir),
		TempDir:     valueOr(raw.TempDir, defaults.TempDir),
		Connections: valueOr(raw.Connections, defaults.Connections),
		Chunks:      valueOr(raw.Chunks, defaults.Chunks),
		MaxRetries:  valueOr(raw.MaxRetries, defaults.MaxRetries),
		RetryDelay:  valueOr(raw.RetryDelay, defaults.RetryDelay),
	}
}

func resolveTorrent(raw *rawTorrentConfig, defaults *TorrentConfig) *TorrentConfig {
	if raw == nil {
		return defaults
	}

	return &TorrentConfig{
		DownloadDir:                      valueOr(raw.DownloadDir, defaults.DownloadDir),
		Seed:                             valueOr(raw.Seed, defaults.Seed),
		EstablishedConnectionsPerTorrent: valueOr(raw.EstablishedConnectionsPerTorrent, defaults.EstablishedConnectionsPerTorrent),
		HalfOpenConnectionsPerTorrent:    valueOr(raw.HalfOpenConnectionsPerTorrent, defaults.HalfOpenConnectionsPerTorrent),
		TotalHalfOpenConnections:         valueOr(raw.TotalHalfOpenConnections, defaults.TotalHalfOpenConnections),
		DisableDHT:                       valueOr(raw.DisableDHT, defaults.DisableDHT),
		DisablePEX:                       valueOr(raw.DisablePEX, defaults.DisablePEX),
		DisableTrackers:                  valueOr(raw.DisableTrackers, defaults.DisableTrackers),
		DisableIPv6:                      valueOr(raw.DisableIPv6, defaults.DisableIPv6),
		MetainfoTimeout:                  valueOr(raw.MetainfoTimeout, defaults.MetainfoTimeout),
	}
}
