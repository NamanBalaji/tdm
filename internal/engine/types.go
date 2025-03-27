package engine

import (
	"os"
	"path/filepath"
)

// Config contains download manager configuration
type Config struct {
	DownloadDir               string
	ConfigDir                 string
	MaxConcurrentDownloads    int
	MaxConnectionsPerDownload int
	ChunkSize                 int64
	UserAgent                 string
	TempDir                   string
	AutoStartDownloads        bool  // Automatically start downloads when added
	MaxRetries                int   // Maximum retry attempts for a chunk
	RetryDelay                int64 // Initial retry delay in seconds
	SaveInterval              int64 // Interval in seconds to save download state
}

// DefaultConfig returns the default engine configuration
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
		MaxConcurrentDownloads:    3,
		MaxConnectionsPerDownload: 8,
		ChunkSize:                 4 * 1024 * 1024, // 4MB
		UserAgent:                 "TDM/1.0",
		TempDir:                   filepath.Join(os.TempDir(), "tdm"),
		AutoStartDownloads:        true,
		MaxRetries:                5,
		RetryDelay:                1,  // 1 second initial retry delay
		SaveInterval:              30, // Save state every 30 seconds
	}
}
