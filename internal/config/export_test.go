package config

import "flag"

func registerTestFlags(fs *flag.FlagSet) {
	fs.String("urls", "", "URLs separated by spaces")
	fs.Int("mcd", 0, "max concurrent downloads")
	fs.String("td", "", "temporary directory for HTTP chunk storage")
	fs.Int("conn", 0, "parallel HTTP connections per download")
	fs.Int("c", 0, "number of chunks per HTTP download")
	fs.Int("mr", -1, "max retries per chunk")
	fs.String("http-dir", "", "HTTP download directory")
	fs.String("torrent-dir", "", "torrent download directory")
	fs.String("dd", "", "download directory for both HTTP and torrent")
	fs.Bool("ns", false, "disable seeding for torrents")
	fs.Bool("np", false, "disable PEX for torrents")
	fs.Bool("nd", false, "disable DHT for torrents")
}
