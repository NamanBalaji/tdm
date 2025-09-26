package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
)

const (
	maxConcurrentDownloads           = 3
	maxRetries                       = 3
	retryDelay                       = 2 * time.Second
	httpChunks                       = 32
	httpConnections                  = 8
	seedTorrent                      = true
	establishedConnectionsPerTorrent = 50
	halfOpenConnectionsPerTorrent    = 25
	totalHalfOpenConnections         = 100
	disableDHT                       = false
	disablePEX                       = false
	disableTrackers                  = false
	disableIPv6                      = false
	metainfoTimeout                  = 60 * time.Second
)

var (
	downloadDir = xdg.UserDirs.Download
	tempDir     = filepath.Join(os.TempDir(), configFileName)
)
