package http

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/NamanBalaji/tdm/internal/logger"
)

func (d *Downloader) merge(st *httpState, dir, filename string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	targetPath := filepath.Join(dir, filename)

	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	defer func() { _ = outFile.Close() }()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer

	// Sort chunks by start byte
	sorted := make([]chunkState, len(st.Chunks))
	copy(sorted, st.Chunks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartByte < sorted[j].StartByte
	})

	for _, c := range sorted {
		chunkFile, err := os.Open(c.TempFilePath)
		if err != nil {
			return fmt.Errorf("failed to open chunk file %s: %w", c.TempFilePath, err)
		}

		_, err = io.Copy(bufWriter, chunkFile)
		_ = chunkFile.Close()

		if err != nil {
			return fmt.Errorf("failed to copy chunk data: %w", err)
		}
	}

	if err := bufWriter.Flush(); err != nil {
		return fmt.Errorf("failed to flush output: %w", err)
	}

	logger.Debugf("merged %d chunks to %s", len(sorted), targetPath)

	return nil
}
