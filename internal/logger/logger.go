// Package logger configures the process-wide slog default logger.
package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	once    sync.Once
	logFile *os.File
)

func Init(debugMode bool, logPath string) error {
	var initErr error

	once.Do(func() {
		if !debugMode || logPath == "" {
			slog.SetDefault(slog.New(slog.DiscardHandler))
			return
		}

		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			initErr = err
			return
		}

		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			initErr = err
			return
		}

		logFile = f
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})))
	})

	return initErr
}

func Close() {
	if logFile != nil {
		_ = logFile.Close()
	}
}
