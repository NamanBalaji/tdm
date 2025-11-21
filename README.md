# 🚀 TDM (Terminal Download Manager)

![Build Status](https://github.com/NamanBalaji/tdm/actions/workflows/ci.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/NamanBalaji/tdm)](https://goreportcard.com/report/github.com/NamanBalaji/tdm)

TDM is a cross-platform, multi protocol fast and lightweight download manager that runs directly in your terminal. Designed for efficiency and ease of use, TDM provides a powerful solution for downloading files with advanced capabilities.

![TDM Terminal Interface](./assets/tdm_recording.gif)

## Multi Protocol

- **HTTP/HTTPS**
  - Multi-connection chunked downloads
  - Automatic fallback to a single connection for unsupported servers

- **BitTorrent** (paste the url that downloads the .torrent file or the magnet link)
  - Torrent file links and magnet links support
  - Peer discovery and management
  - Seeding capabilities
  - Tracker support
  - DHT and PEX support

### Download Management

- Priority based download queueing system
- Pause, resume, cancel, and delete for all downloads
- Comprehensive download status tracking

## 🛠️ Installation

### Pre-built Binaries

- Download the binary from the release page
- For macOS/Linux replace $SRC with your downloaded artifact path and run:

```bash
sudo mv "$SRC" /usr/local/bin/tdm
chmod +x /usr/local/bin/tdm || true
```

- Make sure it's added to your path
- Then simply run tdm from your shell

#### Go Installation

```bash
go install github.com/NamanBalaji/tdm@latest
```

### Install from Source

```bash
git clone https://github.com/NamanBalaji/tdm.git
cd tdm
go build
./tdm
```

## 🔧 Configuration

TDM offers extensive configuration options via a YAML file or CLI flags.

### Config File

Create a config file at `~/.config/tdm` (on Linux/macOS) or `%LOCALAPPDATA%/tdm` (on Windows).

Here is an example configuration with default values:

```yaml
maxConcurrentDownloads: 3

http:
  dir: "/User/Downloads" # Default: User's download directory
  tempDir: "/tmp/tdm" # Default: System temp dir/tdm
  connections: 8 # Connections per download
  maxChunks: 32 # Split download into chunks
  maxRetries: 3 # Retries per chunk failure
  retryDelay: 2s # Delay between retries

torrent:
  dir: "/User/Downloads" # Default: User's download directory
  seed: true # Enable seeding
  establishedConnectionsPerTorrent: 50 # Max established peers per torrent
  halfOpenConnectionsPerTorrent: 25 # Max half-open connections
  totalHalfOpenConnections: 100 # Global max half-open connections
  disableDht: false # Enable/Disable DHT
  disablePex: false # Enable/Disable Peer Exchange
  disableTrackers: false # Enable/Disable Trackers
  disableIPv6: false # Enable/Disable IPv6
  metainfoTimeout: 60s # Timeout for fetching metadata
```

### CLI Flags

CLI flags override config file values.

| Flag  | Description                                         |
| ----- | --------------------------------------------------- |
| -urls | Path to a file containing URLs separated by space   |
| -mcd  | Max number of concurrent downloads                  |
| -dd   | Path to the directory for storing new downloads     |
| -td   | Path to temporary directory for storing HTTP chunks |
| -conn | Number of parallel connections for HTTP downloads   |
| -c    | Number of chunks a HTTP download will be split into |
| -mr   | Maximum number of retries before a chunk fails      |
| -ns   | Disable seeding for torrents                        |
| -np   | Disable Peer Exchange (PEX) for torrents            |
| -nd   | Disable DHT for torrents                            |

## 🗂️ Upcoming Features

- [ ] yt-dlp integration
- [ ] FTP protocol support
- [ ] SFTP protocol support
- [ ] TUI improvements

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull RequestMsg.

### Development Setup

1. Clone the repository
2. Install dependencies: `go mod download`
3. Run tests: `go test ./...`
4. Build: `go build`
