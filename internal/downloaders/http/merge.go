package http

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	sorted := slices.Clone(st.Chunks)
	slices.SortFunc(sorted, func(a, b chunkState) int {
		return cmp.Compare(a.StartByte, b.StartByte)
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

	slog.Debug("merged chunks", "count", len(sorted), "path", targetPath)

	return nil
}
