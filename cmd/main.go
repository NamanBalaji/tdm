package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/engine"
)

// printDownloadProgress prints the current progress of all downloads
func printDownloadProgress(eng *engine.Engine, downloadIDs []uuid.UUID, stopChan <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Clear the current line
	clearLine := func() {
		fmt.Print("\r\033[K") // ANSI escape code to clear the line
	}

	// Generate progress bar
	getProgressBar := func(percentage float64, width int) string {
		completed := int(percentage * float64(width) / 100)
		bar := "["
		for i := 0; i < width; i++ {
			if i < completed {
				bar += "="
			} else {
				bar += " "
			}
		}
		bar += "]"
		return bar
	}

	for {
		select {
		case <-ticker.C:
			clearLine()
			fmt.Print("\n") // Start on a fresh line

			globalStats := eng.GetGlobalStats()
			fmt.Printf("Active: %d, Queued: %d, Speed: %.2f MB/s\n",
				globalStats.ActiveDownloads,
				globalStats.QueuedDownloads,
				float64(globalStats.CurrentSpeed)/(1024*1024))

			for i, id := range downloadIDs {
				download, err := eng.GetDownload(id)
				if err != nil {
					fmt.Printf("[%d] Error getting download: %v\n", i+1, err)
					continue
				}

				stats := download.GetStats()
				progressBar := getProgressBar(stats.Progress, 30)
				fmt.Printf("[%d] %s %s %.1f%% %.2f MB/s %s\n",
					i+1,
					stats.Status,
					progressBar,
					stats.Progress,
					float64(stats.Speed)/(1024*1024),
					download.Filename)
			}

			// Move cursor back up for the next update
			for i := 0; i <= len(downloadIDs); i++ {
				fmt.Print("\033[1A") // Move cursor up one line
			}

		case <-stopChan:
			clearLine()
			// Final update before stopping
			for i, id := range downloadIDs {
				download, err := eng.GetDownload(id)
				if err != nil {
					fmt.Printf("[%d] Error getting download: %v\n", i+1, err)
					continue
				}

				stats := download.GetStats()
				progressBar := getProgressBar(stats.Progress, 30)
				fmt.Printf("[%d] %s %s %.1f%% %.2f MB/s %s\n",
					i+1,
					stats.Status,
					progressBar,
					stats.Progress,
					float64(stats.Speed)/(1024*1024),
					download.Filename)
			}
			return
		}
	}
}

func main() {
	// Create and initialize the engine
	config := engine.DefaultConfig()
	eng, err := engine.New(config)
	if err != nil {
		fmt.Printf("Error creating engine: %v\n", err)
		os.Exit(1)
	}

	if err := eng.Init(); err != nil {
		fmt.Printf("Error initializing engine: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Engine started successfully")

	// downloadIDs := make([]uuid.UUID, 0)
	//
	// for _, d := range eng.ListDownloads() {
	//	err := eng.ResumeDownload(context.Background(), d.ID)
	//	if err != nil {
	//		fmt.Printf("Error resuming download: %v\n", err)
	//	}
	//	downloadIDs = append(downloadIDs, d.ID)
	//}
	// URLs that should work reliably (different file sizes)
	testURLs := []string{
		"https://github.com/cli/cli/releases/download/v2.23.0/gh_2.23.0_linux_amd64.tar.gz",
		"https://storage.googleapis.com/golang/go1.17.6.linux-amd64.tar.gz",
		"https://cdn.jsdelivr.net/npm/bootstrap@5.2.3/dist/css/bootstrap.min.css",
		"https://archive.org/download/stackexchange/academia.stackexchange.com.7z",
		"https://sourceforge.net/projects/sevenzip/files/7-Zip/21.07/7z2107-x64.exe/download",
		// "https://releases.ubuntu.com/22.04/ubuntu-22.04.5-desktop-amd64.iso", // ~10MB file from Apache
		// "https://cdn.kernel.org/pub/linux/kernel/v5.x/linux-5.15.116.tar.xz", // ~115MB file from kernel.org
		// "https://github.com/google/googletest/archive/refs/tags/v1.13.0.zip", // ~1MB file from GitHub
	}

	// Start a few downloads to test shutdown behavior
	var wg sync.WaitGroup
	downloadIDs := make([]uuid.UUID, 0, len(testURLs))

	for _, url := range testURLs {
		wg.Add(1)
		go func(downloadURL string) {
			defer wg.Done()
			id, err := eng.AddDownload(downloadURL, nil)
			if err != nil {
				fmt.Printf("Error adding download %s: %v\n", downloadURL, err)
				return
			}
			downloadIDs = append(downloadIDs, id)
			fmt.Printf("Added download %s with ID: %s\n", downloadURL, id)
		}(url)
	}

	// Wait for all downloads to be added
	wg.Wait()
	fmt.Printf("Added %d downloads\n", len(downloadIDs))

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel to stop progress tracking
	stopProgressChan := make(chan struct{})

	// Start progress tracking in background
	go printDownloadProgress(eng, downloadIDs, stopProgressChan)

	fmt.Println("\nDownloads in progress... Press Ctrl+C to test graceful shutdown")

	// Wait for shutdown signal or timeout for demo
	select {
	case sig := <-sigChan:
		fmt.Printf("\nReceived signal: %v\n", sig)
	case <-time.After(300 * time.Second): // Timeout after 5 minutes for demo
		fmt.Println("\nTimeout reached, initiating shutdown")
	}

	// Stop progress tracking
	close(stopProgressChan)
	time.Sleep(500 * time.Millisecond) // Give the progress tracker time to finish

	// Perform graceful shutdown
	fmt.Println("Starting graceful shutdown...")
	start := time.Now()
	if err := eng.Shutdown(); err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
	}
	elapsed := time.Since(start)
	fmt.Printf("Shutdown completed in %v\n", elapsed)

	// Check final status of downloads after shutdown
	fmt.Println("\nFinal download statuses after shutdown:")
	for i, id := range downloadIDs {
		download, err := eng.GetDownload(id)
		if err != nil {
			fmt.Printf("[%d] Error getting download: %v\n", i+1, err)
			continue
		}

		stats := download.GetStats()
		fmt.Printf("[%d] %s: %.1f%% complete, Status: %s\n",
			i+1,
			download.Filename,
			stats.Progress,
			stats.Status)
	}
}
