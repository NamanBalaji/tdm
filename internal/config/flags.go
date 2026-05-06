package config

import (
	"flag"
	"strings"
)

var (
	flagURLs        = flag.String("urls", "", "URLs separated by spaces")
	flagMCD         = flag.Int("mcd", 0, "max concurrent downloads")
	flagTempDir     = flag.String("td", "", "temporary directory for HTTP chunk storage")
	flagConn        = flag.Int("conn", 0, "parallel HTTP connections per download")
	flagChunks      = flag.Int("c", 0, "number of chunks per HTTP download")
	flagMaxRetries  = flag.Int("mr", -1, "max retries per chunk (-1 = use config/default)")
	flagHTTPDir     = flag.String("http-dir", "", "HTTP download directory")
	flagTorrentDir  = flag.String("torrent-dir", "", "torrent download directory")
	flagDownloadDir = flag.String("dd", "", "download directory for both HTTP and torrent (deprecated: use -http-dir and -torrent-dir)")
	flagNoSeed      = flag.Bool("ns", false, "disable seeding for torrents")
	flagNoPEX       = flag.Bool("np", false, "disable PEX for torrents")
	flagNoDHT       = flag.Bool("nd", false, "disable DHT for torrents")
)

func applyFlags(cfg *Config) {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "urls":
			if s := *flagURLs; s != "" {
				cfg.Urls = strings.Fields(s)
			}
		case "mcd":
			cfg.MaxConcurrentDownloads = *flagMCD
		case "td":
			cfg.HTTP.TempDir = *flagTempDir
		case "conn":
			cfg.HTTP.Connections = *flagConn
		case "c":
			cfg.HTTP.Chunks = *flagChunks
		case "mr":
			cfg.HTTP.MaxRetries = *flagMaxRetries
		case "http-dir":
			cfg.HTTP.DownloadDir = *flagHTTPDir
		case "torrent-dir":
			cfg.Torrent.DownloadDir = *flagTorrentDir
		case "dd":
			cfg.HTTP.DownloadDir = *flagDownloadDir
			cfg.Torrent.DownloadDir = *flagDownloadDir
		case "ns":
			cfg.Torrent.Seed = !*flagNoSeed
		case "np":
			cfg.Torrent.DisablePEX = *flagNoPEX
		case "nd":
			cfg.Torrent.DisableDHT = *flagNoDHT
		}
	})
}

func applyFlagsFromFlagSet(cfg *Config, fs *flag.FlagSet) {
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "urls":
			if s := f.Value.String(); s != "" {
				cfg.Urls = strings.Fields(s)
			}
		case "mcd":
			cfg.MaxConcurrentDownloads = mustInt(f)
		case "td":
			cfg.HTTP.TempDir = f.Value.String()
		case "conn":
			cfg.HTTP.Connections = mustInt(f)
		case "c":
			cfg.HTTP.Chunks = mustInt(f)
		case "mr":
			cfg.HTTP.MaxRetries = mustInt(f)
		case "http-dir":
			cfg.HTTP.DownloadDir = f.Value.String()
		case "torrent-dir":
			cfg.Torrent.DownloadDir = f.Value.String()
		case "dd":
			dir := f.Value.String()
			cfg.HTTP.DownloadDir = dir
			cfg.Torrent.DownloadDir = dir
		case "ns":
			cfg.Torrent.Seed = !mustBool(f)
		case "np":
			cfg.Torrent.DisablePEX = mustBool(f)
		case "nd":
			cfg.Torrent.DisableDHT = mustBool(f)
		}
	})
}

func mustInt(f *flag.Flag) int {
	v, _ := f.Value.(flag.Getter)
	return v.Get().(int)
}

func mustBool(f *flag.Flag) bool {
	v, _ := f.Value.(flag.Getter)
	return v.Get().(bool)
}
