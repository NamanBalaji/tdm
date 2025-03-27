package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"
)

// ErrorCategory represents the general category of an error
type ErrorCategory int

const (
	// ErrorCategoryNetwork represents network-related errors
	ErrorCategoryNetwork ErrorCategory = iota
	// ErrorCategoryIO represents I/O-related errors
	ErrorCategoryIO
	// ErrorCategoryServer represents server-related errors
	ErrorCategoryServer
	// ErrorCategoryClient represents client/configuration-related errors
	ErrorCategoryClient
	// ErrorCategoryContext represents context cancellation errors
	ErrorCategoryContext
	// ErrorCategoryUnknown represents unknown errors
	ErrorCategoryUnknown
)

// DownloadError represents a contextualized error that occurred during a download
type DownloadError struct {
	Err       error         // Original error
	Category  ErrorCategory // General category of the error
	Retryable bool          // Whether the error is potentially retryable
	Context   string        // Additional context about where the error occurred
	Time      time.Time     // When the error occurred
}

// Error implements the error interface
func (e *DownloadError) Error() string {
	return fmt.Sprintf("[%s] %s: %v", e.Category, e.Context, e.Err)
}

// Unwrap returns the wrapped error
func (e *DownloadError) Unwrap() error {
	return e.Err
}

// String returns the error category as a string
func (c ErrorCategory) String() string {
	switch c {
	case ErrorCategoryNetwork:
		return "Network"
	case ErrorCategoryIO:
		return "IO"
	case ErrorCategoryServer:
		return "Server"
	case ErrorCategoryClient:
		return "Client"
	case ErrorCategoryContext:
		return "Context"
	default:
		return "Unknown"
	}
}

// NewDownloadError creates a new download error with context
func NewDownloadError(err error, context string) *DownloadError {
	if err == nil {
		return nil
	}

	category, retryable := categorizeError(err)
	return &DownloadError{
		Err:       err,
		Category:  category,
		Retryable: retryable,
		Context:   context,
		Time:      time.Now(),
	}
}

// categorizeError determines the category and retryability of an error
func categorizeError(err error) (ErrorCategory, bool) {
	if err == nil {
		return ErrorCategoryUnknown, false
	}

	// Check for context cancellation
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCategoryContext, false
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Most network errors are temporary and worth retrying
		return ErrorCategoryNetwork, netErr.Timeout()
	}

	// Check for specific syscall errors
	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		// Common network-related syscall errors
		// ECONNRESET, EPIPE, etc. are typically retryable
		return ErrorCategoryNetwork, true
	}

	if strings.Contains(err.Error(), "HTTP error") || strings.Contains(err.Error(), "status code") {
		if strings.Contains(err.Error(), "50") {
			return ErrorCategoryServer, true
		}
		if strings.Contains(err.Error(), "40") || strings.Contains(err.Error(), "41") {
			return ErrorCategoryClient, false
		}
	}

	// Check for common error strings
	errStr := strings.ToLower(err.Error())

	// Network-related errors
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "tcp") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "reset") {
		return ErrorCategoryNetwork, true
	}

	// I/O-related errors
	if strings.Contains(errStr, "file") ||
		strings.Contains(errStr, "directory") ||
		strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "disk") {
		return ErrorCategoryIO, false
	}

	// Server-related errors
	if strings.Contains(errStr, "server") ||
		strings.Contains(errStr, "overload") {
		return ErrorCategoryServer, true
	}

	// Default to unknown, not retryable
	return ErrorCategoryUnknown, false
}

// calculateBackoff calculates a backoff duration with jitter
func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	// Exponential backoff: 2^retryCount * baseDelay with 25% jitter
	delay := baseDelay * (1 << uint(retryCount))

	// Apply jitter to avoid thundering herd
	jitterFactor := 0.75 + 0.5*float64(time.Now().Nanosecond())/float64(1e9)
	jitter := time.Duration(float64(delay) * jitterFactor)

	// Cap maximum delay at 2 minutes
	maxDelay := 2 * time.Minute
	if jitter > maxDelay {
		jitter = maxDelay
	}

	return jitter
}
