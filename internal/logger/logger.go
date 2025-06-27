package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

var (
	debugLogger *log.Logger

	DebugEnabled = false

	logFile *os.File
)

// InitLogging sets up logging based on configuration.
func InitLogging(debugMode bool, logPath string) error {
	DebugEnabled = debugMode

	if DebugEnabled && logPath != "" {
		logDir := filepath.Dir(logPath)
		err := os.MkdirAll(logDir, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		logFile = f
		debugLogger = log.New(f, "", log.Ldate|log.Ltime|log.Lshortfile)
	}

	return nil
}

// Close closes the log file if open.
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

func Infof(format string, v ...interface{}) {
	if DebugEnabled && debugLogger != nil {
		debugLogger.Printf("[INFO] "+format, v...)
	}
}

// Errorf logs an error message to the file if debug mode is enabled.
func Errorf(format string, v ...interface{}) {
	if DebugEnabled && debugLogger != nil {
		debugLogger.Printf("[ERROR] "+format, v...)
	}
}

func Debugf(format string, v ...interface{}) {
	if DebugEnabled && debugLogger != nil {
		debugLogger.Printf("[DEBUG] "+format, v...)
	}
}

func Warnf(format string, v ...interface{}) {
	if DebugEnabled && debugLogger != nil {
		debugLogger.Printf("[WARNING] "+format, v...)
	}
}
