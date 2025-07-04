package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/NamanBalaji/tdm/internal/tui"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	configDir := filepath.Join(homeDir, ".tdm")

	err = os.MkdirAll(configDir, 0o755)
	if err != nil {
		fmt.Printf("Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	err = logger.InitLogging(*debug, filepath.Join(configDir, "tdm.log"))
	if err != nil {
		fmt.Printf("Warning: Failed to initialize logging: %v\n", err)
	}
	defer logger.Close()

	repo, err := repository.NewBboltRepository(filepath.Join(configDir, "tdm.db"))
	if err != nil {
		logger.Errorf("Error creating repository: %v\n", err)
		os.Exit(1)
	}

	eng := engine.NewEngine(repo, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = eng.Start(ctx)
	if err != nil {
		logger.Errorf("Error starting engine: %v\n", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Infof("\nReceived interrupt signal, shutting down...")
		cancel()
	}()

	err = tui.Run(ctx, eng)
	if err != nil {
		fmt.Printf("TUI Error: %v\n", err)
	}

	logger.Infof("TUI has exited. Shutting down engine...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	err = eng.Shutdown(shutdownCtx)
	if err != nil {
		logger.Errorf("Error during engine shutdown: %v", err)
	}

	eng.Wait()
	logger.Infof("Shutdown complete.")
}
