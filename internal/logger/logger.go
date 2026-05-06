package logger

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	once     sync.Once
	instance = log.New(io.Discard, "", 0)
	logFile  *os.File
)

func InitLogging(debugMode bool, logPath string) error {
	var initErr error

	once.Do(func() {
		if !debugMode || logPath == "" {
			instance = log.New(io.Discard, "", 0)
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
		instance = log.New(f, "", log.Ldate|log.Ltime|log.Lshortfile)
	})

	return initErr
}

func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

func Infof(format string, v ...any)  { instance.Printf("[INFO] "+format, v...) }
func Errorf(format string, v ...any) { instance.Printf("[ERROR] "+format, v...) }
func Debugf(format string, v ...any) { instance.Printf("[DEBUG] "+format, v...) }
func Warnf(format string, v ...any)  { instance.Printf("[WARN] "+format, v...) }
