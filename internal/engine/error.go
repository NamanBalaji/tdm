package engine

import (
	"context"
	"math/rand"
	"time"

	"github.com/NamanBalaji/tdm/internal/errors"
)

// NewDownloadError creates a new download error with resource information
func NewDownloadError(err error, resource string) error {
	if err == nil {
		return nil
	}

	// If it's already a DownloadError, just return it
	var downloadErr *errors.DownloadError
	if errors.As(err, &downloadErr) {
		return err
	}

	// For context cancellation from the context package
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errors.NewContextError(err, resource)
	}

	// For other error types, create a generic error
	return &errors.DownloadError{
		Err:       err,
		Category:  errors.CategoryUnknown,
		Protocol:  errors.ProtocolGeneric,
		Retryable: false,
		Timestamp: time.Now(),
		Resource:  resource,
	}
}

// calculateBackoff calculates a backoff duration with jitter
func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	// Exponential backoff: 2^retryCount * baseDelay
	delay := baseDelay * (1 << uint(retryCount))

	// Apply jitter to avoid thundering herd (between 75% and 125% of computed delay)
	jitterFactor := 0.75 + 0.5*rand.Float64()
	jitter := time.Duration(float64(delay) * jitterFactor)

	// Cap maximum delay at 2 minutes
	maxDelay := 2 * time.Minute
	if jitter > maxDelay {
		jitter = maxDelay
	}

	return jitter
}

// IsRetryableError determines if an error is retryable
// This is just a wrapper around errors.IsRetryable for compatibility
func IsRetryableError(err error) bool {
	return errors.IsRetryable(err)
}

// GetErrorCategory extracts the category from an error
func GetErrorCategory(err error) errors.ErrorCategory {
	var downloadErr *errors.DownloadError
	if errors.As(err, &downloadErr) {
		return downloadErr.Category
	}
	return errors.CategoryUnknown
}

// GetErrorProtocol extracts the protocol from an error
func GetErrorProtocol(err error) errors.Protocol {
	var downloadErr *errors.DownloadError
	if errors.As(err, &downloadErr) {
		return downloadErr.Protocol
	}
	return errors.ProtocolGeneric
}

// AddErrorDetails adds additional context to an error
func AddErrorDetails(err error, details map[string]interface{}) error {
	return errors.WithDetails(err, details)
}
