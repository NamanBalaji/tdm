package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NamanBalaji/tdm/internal/config"
	httpdl "github.com/NamanBalaji/tdm/internal/downloaders/http"
	torrentdl "github.com/NamanBalaji/tdm/internal/downloaders/torrent"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/manager"
	"github.com/NamanBalaji/tdm/internal/store/boltdb"
	"github.com/NamanBalaji/tdm/internal/tui"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")

	flag.Parse()

	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v\n", err)
	}

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".tdm")
	os.MkdirAll(configDir, 0o755)

	logger.InitLogging(*debug, filepath.Join(configDir, "tdm.log"))

	defer logger.Close()

	store, err := boltdb.New(filepath.Join(configDir, "tdm.db"))
	if err != nil {
		log.Fatalf("Error creating store: %v\n", err)
	}
	defer store.Close()

	torrentClient, err := torrentPkg.NewClient(cfg.Torrent)
	if err != nil {
		log.Fatalf("Error creating torrent client: %v\n", err)
	}
	defer torrentClient.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := manager.New(store, cfg.MaxConcurrentDownloads)

	// Register downloaders (order matters — first match wins)
	mgr.Register(torrentdl.New(torrentClient, cfg.Torrent.DownloadDir))
	mgr.Register(httpdl.New(cfg.HTTP))

	if err := mgr.Start(ctx); err != nil {
		log.Fatalf("Error starting manager: %v\n", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	if err := tui.Run(ctx, mgr); err != nil {
		logger.Errorf("TUI error: %v", err)
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := mgr.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Error during shutdown: %v", err)
	}

	mgr.Wait()
}
