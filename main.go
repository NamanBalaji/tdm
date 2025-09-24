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

	"github.com/adrg/xdg"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/tui"
	torrentPkg "github.com/NamanBalaji/tdm/pkg/torrent"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Error getting home directory: %v\n", err)
	}

	configDir := filepath.Join(homeDir, ".tdm")

	err = os.MkdirAll(configDir, 0o755)
	if err != nil {
		log.Fatalf("Error creating config directory: %v\n", err)
	}

	err = logger.InitLogging(*debug, filepath.Join(configDir, "tdm.log"))
	if err != nil {
		log.Fatalf("Warning: Failed to initialize logging: %v\n", err)
	}
	defer logger.Close()

	repo, err := repository.NewBboltRepository(filepath.Join(configDir, "tdm.db"))
	if err != nil {
		log.Fatalf("Error creating repository: %v\n", err)
	}

	torrentClient, err := torrentPkg.NewClient(xdg.UserDirs.Download)
	if err != nil {
		log.Fatalf("Error creating torrent client: %v\n", err)
	}

	defer func() {
		if err := torrentClient.Close(); err != nil {
			log.Printf("Error closing torrent client: %v\n", err)
		}
	}()

	eng := engine.NewEngine(repo, torrentClient, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = eng.Start(ctx)
	if err != nil {
		log.Fatalf("Error starting engine: %v\n", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	err = tui.Run(ctx, eng)
	if err != nil {
		logger.Errorf("TUI Error: %v\n", err)
	}

	logger.Infof("TUI has exited. Shutting down engine...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	err = eng.Shutdown(shutdownCtx)
	if err != nil {
		log.Fatalf("Error during engine shutdown: %v", err)
	}

	eng.Wait()
	logger.Infof("Shutdown complete.")
}
