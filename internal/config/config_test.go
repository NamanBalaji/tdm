package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	cfg "github.com/NamanBalaji/tdm/internal/config"
	"github.com/adrg/xdg"
)

func withTempConfigHome(t *testing.T) (restore func(), dir string, file string) {
	t.Helper()
	orig := xdg.ConfigHome
	dir = t.TempDir()
	xdg.ConfigHome = dir
	restore = func() { xdg.ConfigHome = orig }
	file = filepath.Join(dir, "tdm")
	return
}

func TestGetConfig_Table(t *testing.T) {
	restore, _, cfgFile := withTempConfigHome(t)
	defer restore()

	def := cfg.DefaultConfig()

	tests := []struct {
		name      string
		preWrite  bool
		contents  string
		expectErr bool
		check     func(t *testing.T, got *cfg.Config, def cfg.Config)
	}{
		{
			name:     "missing_file_returns_defaults",
			preWrite: false,
			check: func(t *testing.T, got *cfg.Config, def cfg.Config) {
				if !reflect.DeepEqual(*got, def) {
					t.Fatalf("expected defaults\nwant: %#v\ngot:  %#v", def, *got)
				}
			},
		},
		{
			name:     "empty_file_returns_defaults",
			preWrite: true,
			contents: "",
			check: func(t *testing.T, got *cfg.Config, def cfg.Config) {
				if !reflect.DeepEqual(*got, def) {
					t.Fatalf("expected defaults\nwant: %#v\ngot:  %#v", def, *got)
				}
			},
		},
		{
			name:      "invalid_yaml_returns_error",
			preWrite:  true,
			contents:  ": not yaml",
			expectErr: true,
			check:     func(t *testing.T, _ *cfg.Config, _ cfg.Config) {},
		},
		{
			name:     "no_subconfigs_uses_defaults_for_nested",
			preWrite: true,
			contents: "maxConcurrentDownloads: 1\n",
			check: func(t *testing.T, got *cfg.Config, def cfg.Config) {
				if got.MaxConcurrentDownloads != 1 {
					t.Fatalf("maxConcurrentDownloads not applied, got %d", got.MaxConcurrentDownloads)
				}
				// Http and Torrent should fall back to defaults when nil in file
				if !reflect.DeepEqual(*got.Http, *def.Http) {
					t.Fatalf("http defaults not applied\nwant: %#v\ngot:  %#v", *def.Http, *got.Http)
				}
				if !reflect.DeepEqual(*got.Torrent, *def.Torrent) {
					t.Fatalf("torrent defaults not applied\nwant: %#v\ngot:  %#v", *def.Torrent, *got.Torrent)
				}
			},
		},
		{
			name:     "partial_override_and_fallback",
			preWrite: true,
			contents: `
maxConcurrentDownloads: 333
http:
  connections: 99
  retryDelay: 3s
torrent:
  seed: true
  disableIPv6: true
  metainfoTimeout: 1m
`,
			check: func(t *testing.T, got *cfg.Config, def cfg.Config) {
				// top-level override
				if got.MaxConcurrentDownloads != 333 {
					t.Fatalf("want MaxConcurrentDownloads=333 got %d", got.MaxConcurrentDownloads)
				}
				// http overrides
				if got.Http.Connections != 99 {
					t.Fatalf("want http.connections=99 got %d", got.Http.Connections)
				}
				if got.Http.RetryDelay != 3*time.Second {
					t.Fatalf("want http.retryDelay=3s got %s", got.Http.RetryDelay)
				}
				// http fallbacks
				if got.Http.DownloadDir != def.Http.DownloadDir {
					t.Fatalf("want http.dir default %q got %q", def.Http.DownloadDir, got.Http.DownloadDir)
				}
				if got.Http.TempDir != def.Http.TempDir {
					t.Fatalf("want http.tempDir default %q got %q", def.Http.TempDir, got.Http.TempDir)
				}
				if got.Http.Chunks != def.Http.Chunks {
					t.Fatalf("want http.maxChunks default %d got %d", def.Http.Chunks, got.Http.Chunks)
				}
				if got.Http.MaxRetries != def.Http.MaxRetries {
					t.Fatalf("want http.maxRetries default %d got %d", def.Http.MaxRetries, got.Http.MaxRetries)
				}
				// torrent overrides
				if got.Torrent.Seed != true {
					t.Fatalf("want torrent.seed=true got %v", got.Torrent.Seed)
				}
				if got.Torrent.DisableIPv6 != true {
					t.Fatalf("want torrent.disableIPv6=true got %v", got.Torrent.DisableIPv6)
				}
				if got.Torrent.MetainfoTimeout != time.Minute {
					t.Fatalf("want torrent.metainfoTimeout=1m got %s", got.Torrent.MetainfoTimeout)
				}
				// torrent fallbacks
				if got.Torrent.DownloadDir != def.Torrent.DownloadDir {
					t.Fatalf("want torrent.dir default %q got %q", def.Torrent.DownloadDir, got.Torrent.DownloadDir)
				}
				if got.Torrent.EstablishedConnectionsPerTorrent != def.Torrent.EstablishedConnectionsPerTorrent {
					t.Fatalf("want establishedConnectionsPerTorrent default %d got %d",
						def.Torrent.EstablishedConnectionsPerTorrent, got.Torrent.EstablishedConnectionsPerTorrent)
				}
				if got.Torrent.HalfOpenConnectionsPerTorrent != def.Torrent.HalfOpenConnectionsPerTorrent {
					t.Fatalf("want halfOpenConnectionsPerTorrent default %d got %d",
						def.Torrent.HalfOpenConnectionsPerTorrent, got.Torrent.HalfOpenConnectionsPerTorrent)
				}
				if got.Torrent.TotalHalfOpenConnections != def.Torrent.TotalHalfOpenConnections {
					t.Fatalf("want totalHalfOpenConnections default %d got %d",
						def.Torrent.TotalHalfOpenConnections, got.Torrent.TotalHalfOpenConnections)
				}
				if got.Torrent.DisableDHT != def.Torrent.DisableDHT {
					t.Fatalf("want disableDht default %v got %v", def.Torrent.DisableDHT, got.Torrent.DisableDHT)
				}
				if got.Torrent.DisablePEX != def.Torrent.DisablePEX {
					t.Fatalf("want disablePex default %v got %v", def.Torrent.DisablePEX, got.Torrent.DisablePEX)
				}
			},
		},
		{
			name:     "explicit_zero_values_fall_back_to_defaults",
			preWrite: true,
			contents: `
http:
  connections: 0
  dir: ""
  retryDelay: 0s
torrent:
  establishedConnectionsPerTorrent: 0
  halfOpenConnectionsPerTorrent: 0
  totalHalfOpenConnections: 0
  disableDht: false
  disablePex: false
  disableTrackers: false
  disableIPv6: false
  metainfoTimeout: 0s
`,
			check: func(t *testing.T, got *cfg.Config, def cfg.Config) {
				// http zeros fall back
				if got.Http.Connections != def.Http.Connections {
					t.Fatalf("http.connections zero should fallback. want %d got %d", def.Http.Connections, got.Http.Connections)
				}
				if got.Http.DownloadDir != def.Http.DownloadDir {
					t.Fatalf("http.dir zero should fallback. want %q got %q", def.Http.DownloadDir, got.Http.DownloadDir)
				}
				if got.Http.RetryDelay != def.Http.RetryDelay {
					t.Fatalf("http.retryDelay zero should fallback. want %s got %s", def.Http.RetryDelay, got.Http.RetryDelay)
				}
				// torrent zeros fall back
				if got.Torrent.EstablishedConnectionsPerTorrent != def.Torrent.EstablishedConnectionsPerTorrent {
					t.Fatalf("establishedConnectionsPerTorrent zero should fallback. want %d got %d",
						def.Torrent.EstablishedConnectionsPerTorrent, got.Torrent.EstablishedConnectionsPerTorrent)
				}
				if got.Torrent.HalfOpenConnectionsPerTorrent != def.Torrent.HalfOpenConnectionsPerTorrent {
					t.Fatalf("halfOpenConnectionsPerTorrent zero should fallback. want %d got %d",
						def.Torrent.HalfOpenConnectionsPerTorrent, got.Torrent.HalfOpenConnectionsPerTorrent)
				}
				if got.Torrent.TotalHalfOpenConnections != def.Torrent.TotalHalfOpenConnections {
					t.Fatalf("totalHalfOpenConnections zero should fallback. want %d got %d",
						def.Torrent.TotalHalfOpenConnections, got.Torrent.TotalHalfOpenConnections)
				}
				// booleans are zero when false, so they should fallback to defaults as well
				if got.Torrent.DisableDHT != def.Torrent.DisableDHT {
					t.Fatalf("disableDht false should fallback. want %v got %v", def.Torrent.DisableDHT, got.Torrent.DisableDHT)
				}
				if got.Torrent.DisablePEX != def.Torrent.DisablePEX {
					t.Fatalf("disablePex false should fallback. want %v got %v", def.Torrent.DisablePEX, got.Torrent.DisablePEX)
				}
				if got.Torrent.DisableTrackers != def.Torrent.DisableTrackers {
					t.Fatalf("disableTrackers false should fallback. want %v got %v", def.Torrent.DisableTrackers, got.Torrent.DisableTrackers)
				}
				if got.Torrent.DisableIPv6 != def.Torrent.DisableIPv6 {
					t.Fatalf("disableIPv6 false should fallback. want %v got %v", def.Torrent.DisableIPv6, got.Torrent.DisableIPv6)
				}
				if got.Torrent.MetainfoTimeout != def.Torrent.MetainfoTimeout {
					t.Fatalf("metainfoTimeout zero should fallback. want %s got %s", def.Torrent.MetainfoTimeout, got.Torrent.MetainfoTimeout)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// clean start each subtest
			_ = os.Remove(cfgFile)
			if tc.preWrite {
				if err := os.WriteFile(cfgFile, []byte(tc.contents), 0o600); err != nil {
					t.Fatalf("write test config: %v", err)
				}
			}
			got, err := cfg.GetConfig()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetConfig error: %v", err)
			}
			tc.check(t, got, def)
		})
	}
}

func TestDefaultConfig_NonNilPointers(t *testing.T) {
	d := cfg.DefaultConfig()
	if d.Http == nil {
		t.Fatalf("DefaultConfig.Http is nil")
	}
	if d.Torrent == nil {
		t.Fatalf("DefaultConfig.Torrent is nil")
	}
}

func TestIsConfigMarkers(t *testing.T) {
	t.Run("HttpConfig_IsConfig", func(t *testing.T) {
		var h cfg.HttpConfig
		if !h.IsConfig() {
			t.Fatalf("HttpConfig.IsConfig() = false, want true")
		}
	})
	t.Run("TorrentConfig_IsConfig", func(t *testing.T) {
		var tt cfg.TorrentConfig
		if !tt.IsConfig() {
			t.Fatalf("TorrentConfig.IsConfig() = false, want true")
		}
	})
}
