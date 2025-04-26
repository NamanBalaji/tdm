package engine

import (
	"os"
	"path/filepath"
)

// Config contains download manager configuration.
type Config struct {
	DownloadDir               string
	ConfigDir                 string
	MaxConcurrentDownloads    int
	MaxConnectionsPerDownload int
	MaxConnectionsPerHost     int
	ChunkSize                 int64
	UserAgent                 string
	TempDir                   string
	AutoStartDownloads        bool
	MaxRetries                int
	RetryDelay                int64
	SaveInterval              int64
}

// DefaultConfig returns the default engine configuration.
func DefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	downloadDir := filepath.Join(homeDir, "Downloads")
	configDir := filepath.Join(homeDir, ".tdm")

	return &Config{
		DownloadDir:               downloadDir,
		ConfigDir:                 configDir,
		MaxConcurrentDownloads:    4,
		MaxConnectionsPerDownload: 16,
		MaxConnectionsPerHost:     32,
		ChunkSize:                 4 * 1024 * 1024,
		UserAgent:                 "TDM/1.0",
		TempDir:                   filepath.Join(os.TempDir(), "tdm"),
		AutoStartDownloads:        true,
		MaxRetries:                5,
		RetryDelay:                1,
		SaveInterval:              30, // Save state every 30 seconds
	}
}
