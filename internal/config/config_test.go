package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.MaxConcurrentDownloads != 3 {
		t.Errorf("expected MaxConcurrentDownloads 3, got %d", cfg.MaxConcurrentDownloads)
	}

	if cfg.HTTP.Connections != 8 {
		t.Errorf("expected HTTP Connections 8, got %d", cfg.HTTP.Connections)
	}

	if cfg.Torrent.EstablishedConnectionsPerTorrent != 50 {
		t.Errorf("expected Torrent connections 50, got %d", cfg.Torrent.EstablishedConnectionsPerTorrent)
	}

	if cfg.HTTP.RetryDelay != 2*time.Second {
		t.Errorf("expected HTTP RetryDelay 2s, got %v", cfg.HTTP.RetryDelay)
	}

	if !cfg.Torrent.Seed {
		t.Error("expected Torrent.Seed to be true by default")
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 3 {
		t.Errorf("expected defaults when file missing, got %d", cfg.MaxConcurrentDownloads)
	}
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 3 {
		t.Errorf("expected defaults when file empty, got %d", cfg.MaxConcurrentDownloads)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	t.Parallel()

	yamlContent := `
maxConcurrentDownloads: 10
http:
  connections: 20
  maxRetries: 5
torrent:
  disableDht: true
`
	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 10 {
		t.Errorf("expected 10, got %d", cfg.MaxConcurrentDownloads)
	}

	if cfg.HTTP.Connections != 20 {
		t.Errorf("expected 20, got %d", cfg.HTTP.Connections)
	}

	if !cfg.Torrent.DisableDHT {
		t.Error("expected DisableDHT to be true")
	}

	if cfg.HTTP.Chunks != 32 {
		t.Errorf("expected HTTP Chunks to remain default 32, got %d", cfg.HTTP.Chunks)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte("http:\n\tconnections: 5"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Error("expected YAML unmarshal error, got nil")
	}
}

func TestLoadConfig_BoolZeroValue(t *testing.T) {
	t.Parallel()

	yamlContent := `
torrent:
  seed: false
`
	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Torrent.Seed {
		t.Error("expected Seed to be false when explicitly set in YAML")
	}
}

func TestLoadConfig_OmittedFieldsGetDefaults(t *testing.T) {
	t.Parallel()

	yamlContent := `
torrent:
  disableDht: true
`
	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := cfg.validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 3 {
		t.Errorf("expected default 3, got %d", cfg.MaxConcurrentDownloads)
	}

	if cfg.HTTP.Chunks != 32 {
		t.Errorf("expected default 32, got %d", cfg.HTTP.Chunks)
	}

	if cfg.Torrent.EstablishedConnectionsPerTorrent != 50 {
		t.Errorf("expected default 50, got %d", cfg.Torrent.EstablishedConnectionsPerTorrent)
	}
}

func TestLoadConfig_ExplicitZeroFailsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		yamlContent string
		field       string
	}{
		{
			name:        "MaxConcurrentDownloads 0",
			yamlContent: "maxConcurrentDownloads: 0",
			field:       "maxConcurrentDownloads",
		},
		{
			name:        "HTTP Chunks 0",
			yamlContent: "http:\n  maxChunks: 0",
			field:       "http.maxChunks",
		},
		{
			name:        "Torrent EstablishedConns 0",
			yamlContent: "torrent:\n  establishedConnectionsPerTorrent: 0",
			field:       "torrent.establishedConnectionsPerTorrent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "tdm")
			if err := os.WriteFile(path, []byte(tt.yamlContent), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, err := loadConfig(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			err = cfg.validate()
			if err == nil {
				t.Fatal("expected validation error for explicit zero")
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			if ve.Field != tt.field {
				t.Errorf("expected field %q, got %q", tt.field, ve.Field)
			}
		})
	}
}

func TestValidation_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cfg   Config
		field string
	}{
		{
			name:  "MaxConcurrentDownloads zero",
			cfg:   withDefaults(func(c *Config) { c.MaxConcurrentDownloads = 0 }),
			field: "maxConcurrentDownloads",
		},
		{
			name:  "HTTP DownloadDir empty",
			cfg:   withDefaults(func(c *Config) { c.HTTP.DownloadDir = "" }),
			field: "http.dir",
		},
		{
			name:  "HTTP Connections zero",
			cfg:   withDefaults(func(c *Config) { c.HTTP.Connections = 0 }),
			field: "http.connections",
		},
		{
			name:  "HTTP Chunks zero",
			cfg:   withDefaults(func(c *Config) { c.HTTP.Chunks = 0 }),
			field: "http.maxChunks",
		},
		{
			name:  "HTTP MaxRetries negative",
			cfg:   withDefaults(func(c *Config) { c.HTTP.MaxRetries = -1 }),
			field: "http.maxRetries",
		},
		{
			name:  "HTTP TempDir empty",
			cfg:   withDefaults(func(c *Config) { c.HTTP.TempDir = "" }),
			field: "http.tempDir",
		},
		{
			name:  "Torrent DownloadDir empty",
			cfg:   withDefaults(func(c *Config) { c.Torrent.DownloadDir = "" }),
			field: "torrent.dir",
		},
		{
			name:  "Torrent EstablishedConns zero",
			cfg:   withDefaults(func(c *Config) { c.Torrent.EstablishedConnectionsPerTorrent = 0 }),
			field: "torrent.establishedConnectionsPerTorrent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.validate()
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			if !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("expected error to wrap ErrInvalidConfig, got %v", err)
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidationError, got %T", err)
			}

			if ve.Field != tt.field {
				t.Errorf("expected field %q, got %q", tt.field, ve.Field)
			}
		})
	}
}

func TestLoadConfigWithFlags_Override(t *testing.T) {
	t.Parallel()

	yamlContent := `
maxConcurrentDownloads: 5
http:
  connections: 5
`
	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerTestFlags(fs)

	if err := fs.Parse([]string{"-mcd", "50", "-conn", "100", "-urls", "http://example.com"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigWithFlags(path, fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 50 {
		t.Errorf("expected 50, got %d", cfg.MaxConcurrentDownloads)
	}

	if cfg.HTTP.Connections != 100 {
		t.Errorf("expected 100, got %d", cfg.HTTP.Connections)
	}

	if len(cfg.Urls) != 1 || cfg.Urls[0] != "http://example.com" {
		t.Errorf("expected [http://example.com], got %v", cfg.Urls)
	}
}

func TestLoadConfigWithFlags_PartialFlags(t *testing.T) {
	t.Parallel()

	yamlContent := `maxConcurrentDownloads: 15`
	path := filepath.Join(t.TempDir(), "tdm")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerTestFlags(fs)

	if err := fs.Parse([]string{"-c", "99"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigWithFlags(path, fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 15 {
		t.Errorf("expected file value 15, got %d", cfg.MaxConcurrentDownloads)
	}

	if cfg.HTTP.Chunks != 99 {
		t.Errorf("expected flag value 99, got %d", cfg.HTTP.Chunks)
	}
}

func TestLoadConfigWithFlags_NoFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerTestFlags(fs)

	if err := fs.Parse([]string{"-mcd", "50"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigWithFlags(path, fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 50 {
		t.Errorf("expected 50, got %d", cfg.MaxConcurrentDownloads)
	}
}

func TestLoadConfigWithFlags_NoSeed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerTestFlags(fs)

	if err := fs.Parse([]string{"-ns"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigWithFlags(path, fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Torrent.Seed {
		t.Error("expected Seed to be false when -ns flag is set")
	}
}

func TestLoadConfigWithFlags_ValidationError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerTestFlags(fs)

	if err := fs.Parse([]string{"-mcd", "0"}); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfigWithFlags(path, fs)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}
}

func withDefaults(modify func(*Config)) Config {
	cfg := DefaultConfig()
	modify(&cfg)

	return cfg
}
