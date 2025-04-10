package main

import (
	"flag"
	"fmt"
	"github.com/NamanBalaji/tdm/internal/logger"
	"os"
	"os/signal"
	"syscall"

	"github.com/NamanBalaji/tdm/internal/engine"
	"github.com/NamanBalaji/tdm/internal/tui"
)

func main() {
	config := engine.DefaultConfig()

	// Initialize logging
	debug := flag.Bool("debug", false, "debug flag")
	flag.Parse()

	if err := logger.InitLogging(*debug, config.ConfigDir+"/tdm.log"); err != nil {
		fmt.Printf("Warning: Failed to initialize logging: %v\n", err)
	}
	defer logger.Close()

	// Create and initialize the engine
	eng, err := engine.New(config)
	if err != nil {
		fmt.Printf("Error creating engine: %v\n", err)
		os.Exit(1)
	}

	// Initialize the engine
	if err := eng.Init(); err != nil {
		fmt.Printf("Error initializing engine: %v\n", err)
		os.Exit(1)
	}

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

	if err := tui.Run(eng); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
		if err := eng.Shutdown(); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		os.Exit(1)
	}

	if err := eng.Shutdown(); err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
		os.Exit(1)
	}
}
