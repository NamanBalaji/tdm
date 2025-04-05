package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/logger"
	"github.com/NamanBalaji/tdm/internal/tui"
)

func main() {
	// Create and initialize the engine
	config := engine.DefaultConfig()
	eng, err := engine.New(config)
	if err != nil {
		fmt.Printf("Error creating engine: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	if err := logger.InitLogging(true, config.ConfigDir+"/tdm.log"); err != nil {
		fmt.Printf("Warning: Failed to initialize logging: %v\n", err)
	}
	defer logger.Close()

	// Initialize the engine
	if err := eng.Init(); err != nil {
		fmt.Printf("Error initializing engine: %v\n", err)
		os.Exit(1)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Handle signals in a goroutine
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down...")
		if err := eng.Shutdown(); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		os.Exit(0)
	}()

	// Start the TUI
	if err := tui.Run(eng); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
		if err := eng.Shutdown(); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		os.Exit(1)
	}

	// If TUI exited normally, shutdown the engine
	if err := eng.Shutdown(); err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
		os.Exit(1)
	}
}
