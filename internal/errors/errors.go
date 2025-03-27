package errors

import (
	"errors"
	"fmt"
	"time"
)

var (
	Is     = errors.Is
	As     = errors.As
	New    = errors.New
	Unwrap = errors.Unwrap
)

type ErrorCategory string

const (
	CategoryNetwork  ErrorCategory = "NETWORK"  // Connection issues
	CategoryProtocol ErrorCategory = "PROTOCOL" // Protocol-specific errors
	CategoryIO       ErrorCategory = "IO"       // File system issues
	CategoryResource ErrorCategory = "RESOURCE" // Resource not found, etc.
	CategorySecurity ErrorCategory = "SECURITY" // Auth, permissions, etc.
	CategoryContext  ErrorCategory = "CONTEXT"  // Context cancellation
	CategoryUnknown  ErrorCategory = "UNKNOWN"  // Unclassified errors
)

// Protocol identifiers
type Protocol string

const (
	ProtocolHTTP    Protocol = "HTTP"
	ProtocolGeneric Protocol = "GENERIC"
)

// DownloadError represents an error that occurred during download operations
type DownloadError struct {
	Err        error         // Original error
	Category   ErrorCategory // General category
	Protocol   Protocol      // Which protocol generated this error
	Retryable  bool          // Whether retry is recommended
	Timestamp  time.Time     // When the error occurred
	Resource   string        // What resource was being accessed
	StatusCode int           // HTTP status code or protocol equivalent
	Details    map[string]interface{}
}

// Error implements the error interface
func (e *DownloadError) Error() string {
	if e.Protocol == ProtocolGeneric {
		return fmt.Sprintf("[%s] %s: %v", e.Category, e.Resource, e.Err)
	}
	return fmt.Sprintf("[%s:%s] %s (status: %d): %v", e.Protocol, e.Category, e.Resource, e.StatusCode, e.Err)
}

// Unwrap provides the underlying cause for error unwrapping (compatible with errors.As)
func (e *DownloadError) Unwrap() error {
	return e.Err
}

// Common sentinel errors
var (
	ErrUnsupportedProtocol = New("unsupported protocol")
	ErrInvalidURL          = New("invalid URL")
	ErrTimeout             = New("operation timed out")
	ErrConnectionReset     = New("connection reset")
	ErrResourceNotFound    = New("resource not found")
	ErrAccessDenied        = New("access denied")
	ErrAuthentication      = New("authentication required")
)

// NewNetworkError creates a network-related error
func NewNetworkError(err error, resource string, retryable bool) *DownloadError {
	return &DownloadError{
		Err:       err,
		Category:  CategoryNetwork,
		Protocol:  ProtocolGeneric,
		Retryable: retryable,
		Timestamp: time.Now(),
		Resource:  resource,
	}
}

// NewIOError creates an I/O related error
func NewIOError(err error, resource string) *DownloadError {
	return &DownloadError{
		Err:       err,
		Category:  CategoryIO,
		Protocol:  ProtocolGeneric,
		Retryable: false, // I/O errors are generally not retryable
		Timestamp: time.Now(),
		Resource:  resource,
	}
}

// NewContextError creates a context cancellation error
func NewContextError(err error, resource string) *DownloadError {
	return &DownloadError{
		Err:       err,
		Category:  CategoryContext,
		Protocol:  ProtocolGeneric,
		Retryable: false,
		Timestamp: time.Now(),
		Resource:  resource,
	}
}

// NewHTTPError creates an HTTP-specific error
func NewHTTPError(err error, resource string, statusCode int) *DownloadError {
	retryable := false
	category := CategoryProtocol

	switch {
	case statusCode >= 500 && statusCode != 501:
		retryable = true
	case statusCode == 429:
		retryable = true
	case statusCode >= 400:
		category = CategoryResource
	}

	return &DownloadError{
		Err:        err,
		Category:   category,
		Protocol:   ProtocolHTTP,
		Retryable:  retryable,
		Timestamp:  time.Now(),
		Resource:   resource,
		StatusCode: statusCode,
	}
}

// IsRetryable determines if an error should be retried
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var downloadErr *DownloadError
	if As(err, &downloadErr) {
		return downloadErr.Retryable
	}

	return false
}

// IsNetworkError determines if the error is network-related
func IsNetworkError(err error) bool {
	var downloadErr *DownloadError
	return As(err, &downloadErr) && downloadErr.Category == CategoryNetwork
}

// IsIOError determines if the error is I/O related
func IsIOError(err error) bool {
	var downloadErr *DownloadError
	return As(err, &downloadErr) && downloadErr.Category == CategoryIO
}

// IsProtocolError determines if the error is protocol-specific
func IsProtocolError(err error, protocol Protocol) bool {
	var downloadErr *DownloadError
	return As(err, &downloadErr) && downloadErr.Protocol == protocol
}

// GetStatusCode extracts the status code from an error if available
func GetStatusCode(err error) (int, bool) {
	var downloadErr *DownloadError
	if As(err, &downloadErr) {
		return downloadErr.StatusCode, true
	}
	return 0, false
}

// WithDetails adds additional context to a DownloadError
func WithDetails(err error, details map[string]interface{}) error {
	var downloadErr *DownloadError
	if !As(err, &downloadErr) {
		return err
	}

	if downloadErr.Details == nil {
		downloadErr.Details = make(map[string]interface{})
	}

	for k, v := range details {
		downloadErr.Details[k] = v
	}

	return downloadErr
}
