package config_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/adrg/xdg"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func mockXDG(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	oldConfigHome := xdg.ConfigHome

	xdg.ConfigHome = tmpDir

	t.Cleanup(func() {
		xdg.ConfigHome = oldConfigHome
	})

	return tmpDir
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()

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
	if cfg.HTTP.IsConfig() != true {
		t.Error("expected HTTP IsConfig to be true")
	}
	if cfg.Torrent.IsConfig() != true {
		t.Error("expected Torrent IsConfig to be true")
	}
}

func TestGetConfig_Integration(t *testing.T) {
	t.Run("No Config File Returns Defaults", func(t *testing.T) {
		mockXDG(t)
		resetFlags()

		oldArgs := os.Args
		os.Args = []string{"cmd"}
		defer func() { os.Args = oldArgs }()

		cfg, err := config.GetConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.MaxConcurrentDownloads != 3 {
			t.Errorf("expected defaults when file missing, got %d", cfg.MaxConcurrentDownloads)
		}
	})

	t.Run("Empty Config File Returns Defaults", func(t *testing.T) {
		tmpDir := mockXDG(t)
		resetFlags()

		oldArgs := os.Args
		os.Args = []string{"cmd"}
		defer func() { os.Args = oldArgs }()

		err := os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte(""), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := config.GetConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.MaxConcurrentDownloads != 3 {
			t.Errorf("expected defaults when file empty")
		}
	})

	t.Run("Valid Config File Overrides Defaults", func(t *testing.T) {
		tmpDir := mockXDG(t)
		resetFlags()

		oldArgs := os.Args
		os.Args = []string{"cmd"}
		defer func() { os.Args = oldArgs }()

		yamlContent := `
maxConcurrentDownloads: 10
http:
  connections: 20
  maxRetries: 5
torrent:
  disableDht: true
`
		err := os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte(yamlContent), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := config.GetConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.MaxConcurrentDownloads != 10 {
			t.Errorf("expected MaxConcurrentDownloads 10, got %d", cfg.MaxConcurrentDownloads)
		}
		if cfg.HTTP.Connections != 20 {
			t.Errorf("expected HTTP connections 20, got %d", cfg.HTTP.Connections)
		}
		if !cfg.Torrent.DisableDHT {
			t.Error("expected DisableDHT to be true")
		}
		if cfg.HTTP.Chunks != 32 {
			t.Errorf("expected HTTP Chunks to remain default 32, got %d", cfg.HTTP.Chunks)
		}
	})

	t.Run("Invalid YAML Content", func(t *testing.T) {
		tmpDir := mockXDG(t)
		resetFlags()

		oldArgs := os.Args
		os.Args = []string{"cmd"}
		defer func() { os.Args = oldArgs }()

		// Illegal YAML (tab character)
		err := os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte("http:\n\tconnections: 5"), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		_, err = config.GetConfig()
		if err == nil {
			t.Error("expected YAML unmarshal error, got nil")
		}
	})
}

func TestConfig_AutoCorrection(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
	}{
		{
			name:        "MaxConcurrentDownloads 0 becomes Default",
			yamlContent: "maxConcurrentDownloads: 0",
		},
		{
			name:        "HTTP DownloadDir Empty becomes Default",
			yamlContent: "http:\n  dir: \"\"",
		},
		{
			name:        "HTTP Chunks 0 becomes Default",
			yamlContent: "http:\n  maxChunks: 0",
		},
		{
			name:        "Torrent DownloadDir Empty becomes Default",
			yamlContent: "torrent:\n  dir: \"\"",
		},
		{
			name:        "Torrent Established Connections 0 becomes Default",
			yamlContent: "torrent:\n  establishedConnectionsPerTorrent: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := mockXDG(t)
			resetFlags()

			oldArgs := os.Args
			os.Args = []string{"cmd"}
			defer func() { os.Args = oldArgs }()

			err := os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte(tt.yamlContent), 0o644)
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := config.GetConfig()
			if err != nil {
				t.Errorf("expected success (auto-corrected to default), got error: %v", err)
			}
			if cfg != nil && cfg.MaxConcurrentDownloads == 0 {
				t.Error("expected MaxConcurrentDownloads to be corrected to > 0")
			}
		})
	}
}

func TestConfig_Validation_Errors(t *testing.T) {
	tests := []struct {
		name        string
		flags       []string
		yamlContent string
		wantErr     error
	}{
		{
			name:    "Flag Force MaxConcurrentDownloads 0",
			flags:   []string{"-mcd", "0"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:        "YAML Negative MaxRetries (Passed through by zeroOr)",
			yamlContent: "http:\n  maxRetries: -1",
			wantErr:     config.ErrInvalidConfig,
		},
		{
			name:    "Flag Force HTTP Connections 0",
			flags:   []string{"-conn", "0"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "Flag Force HTTP Chunks 0",
			flags:   []string{"-c", "0"},
			wantErr: config.ErrInvalidConfig,
		},
		{
			name:    "Flag Force HTTP DownloadDir Empty",
			flags:   []string{"-dd", ""},
			wantErr: config.ErrInvalidConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := mockXDG(t)
			resetFlags()

			if tt.yamlContent != "" {
				os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte(tt.yamlContent), 0o644)
			}

			// Setup Flags
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()
			args := []string{"cmd"}
			args = append(args, tt.flags...)
			os.Args = args

			_, err := config.GetConfig()
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGetConfig_Flags_OverrideFile(t *testing.T) {
	tmpDir := mockXDG(t)
	resetFlags()

	os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte("maxConcurrentDownloads: 5\nhttp:\n  connections: 5"), 0o644)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"cmd",
		"-mcd", "50",
		"-conn", "100",
		"-urls", "http://example.com",
	}

	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Flags beat Config File (5 -> 50)
	if cfg.MaxConcurrentDownloads != 50 {
		t.Errorf("flag value should overwrite config file. Expected 50, got %d", cfg.MaxConcurrentDownloads)
	}
	if cfg.HTTP.Connections != 100 {
		t.Errorf("flag value should overwrite config file. Expected 100, got %d", cfg.HTTP.Connections)
	}
	// Check URL parsing
	if len(cfg.Urls) != 1 || cfg.Urls[0] != "http://example.com" {
		t.Errorf("expected parsed URLs, got %v", cfg.Urls)
	}
}

func TestGetConfig_Flags_NoFile(t *testing.T) {
	mockXDG(t)
	resetFlags()

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{
		"cmd",
		"-mcd", "50",
	}

	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.MaxConcurrentDownloads != 50 {
		t.Errorf("flag value should be applied even if config file is missing. Expected 50, got %d", cfg.MaxConcurrentDownloads)
	}
}

func TestGetConfig_PartialFlags(t *testing.T) {
	tmpDir := mockXDG(t)
	resetFlags()

	yamlContent := `maxConcurrentDownloads: 15`
	os.WriteFile(filepath.Join(tmpDir, "tdm"), []byte(yamlContent), 0o644)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-c", "99"}

	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MaxConcurrentDownloads != 15 {
		t.Errorf("expected config file value 15 to persist, got %d", cfg.MaxConcurrentDownloads)
	}
	if cfg.HTTP.Chunks != 99 {
		t.Errorf("expected flag value 99, got %d", cfg.HTTP.Chunks)
	}
}
