// Replace your main.go with this simplified approach
// This avoids the parallel download issue entirely

package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// URL to download
	url := "https://www.gutenberg.org/files/1342/1342-0.txt" // Pride and Prejudice

	// Set download directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	downloadDir := filepath.Join(homeDir, "Downloads", "tdm-downloads")
	os.MkdirAll(downloadDir, 0755)

	outputPath := filepath.Join(downloadDir, "pride_and_prejudice.txt")

	// Create the HTTP client
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0")

	// Send the request
	log.Printf("Starting download from %s to %s", url, outputPath)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected status: %s", resp.Status)
	}

	// Create output file
	out, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("Error creating output file: %v", err)
	}
	defer out.Close()

	// Copy with progress monitoring
	fileSize := resp.ContentLength
	log.Printf("Starting download of %d bytes", fileSize)

	var downloaded int64
	buffer := make([]byte, 32*1024)
	start := time.Now()
	lastReport := start

	for {
		n, err := resp.Body.Read(buffer)

		if n > 0 {
			downloaded += int64(n)
			if _, err := out.Write(buffer[:n]); err != nil {
				log.Fatalf("Error writing to file: %v", err)
			}

			// Report progress periodically
			now := time.Now()
			if now.Sub(lastReport) > 500*time.Millisecond {
				lastReport = now
				percent := float64(downloaded) * 100 / float64(fileSize)
				elapsed := now.Sub(start).Seconds()
				speed := float64(downloaded) / elapsed

				fmt.Printf("\rDownload progress: %.2f%% (%d/%d bytes) - %.2f KB/sec",
					percent, downloaded, fileSize, speed/1024)
			}
		}

		if err != nil {
			if err == io.EOF {
				// Download complete
				elapsed := time.Since(start)
				speed := float64(downloaded) / elapsed.Seconds()
				fmt.Printf("\nDownload complete: %d bytes in %.1f seconds (%.2f KB/sec)\n",
					downloaded, elapsed.Seconds(), speed/1024)
				break
			}
			log.Fatalf("Error reading from response: %v", err)
		}
	}
}
