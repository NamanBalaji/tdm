# 🚀 TDM (Terminal Download Manager)
![Build Status](https://github.com/NamanBalaji/tdm/actions/workflows/ci.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/NamanBalaji/tdm)](https://goreportcard.com/report/github.com/NamanBalaji/tdm)

TDM is a cross-platform, multi protocol fast and lightweight download manager that runs directly in your terminal. Designed for efficiency and ease of use, TDM provides a powerful solution for downloading files with advanced capabilities.

![TDM Terminal Interface](./assets/tdm_recording.gif)

### Multi Protocol
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
- Add it to your path 
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

TDM offers extensive configuration options:

- Create the config file at `~/.config/tdm`
- Take a look into .tdm.example for the config structure and config keys 
- Add the configs as per your requirements the ones you don't wanna tweak can be removed TDM will just assume the default values

## 🗂️ Upcoming Features

- [ ] FTP protocol support
- [ ] TUI improvements

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull RequestMsg.

### Development Setup
1. Clone the repository
2. Install dependencies: `go mod download`
3. Run tests: `go test ./...`
4. Build: `go build`
