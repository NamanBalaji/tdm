package http

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Error represents an error related to HTTP operations, including a status code.
type Error struct {
	StatusCode int
	Message    string
}

// Error implements the error interface for HTTPError.
func (e *Error) Error() string {
	return fmt.Sprintf("HTTP error %d: %s", e.StatusCode, e.Message)
}

// NetworkError represents a network-related error with a kind and an optional cause.
type NetworkError struct {
	Kind    string // e.g., "timeout", "reset", "dns", "tls"
	Message string
	Cause   error
}

// Error implements the error interface for NetworkError.
func (e *NetworkError) Error() string {
	return e.Message
}

// Unwrap provides the underlying cause for error unwrapping (compatible with errors.As).
func (e *NetworkError) Unwrap() error {
	return e.Cause
}

// Network error kinds for categorization.
const (
	NetworkErrorTimeout = "timeout"
	NetworkErrorReset   = "reset"
	NetworkErrorDNS     = "dns"
	NetworkErrorTLS     = "tls"
)

// Predefined HTTP error instances for specific, commonly checked errors.
// These are pointers to Error so that errors.Is can compare them by pointer equality.
var (
	ErrHeadNotSupported   = &Error{http.StatusMethodNotAllowed, "HEAD method not supported by server"}
	ErrRangesNotSupported = &Error{http.StatusRequestedRangeNotSatisfiable, "byte ranges not supported by server"}
	ErrResourceNotFound   = &Error{http.StatusNotFound, "resource not found"}
	ErrAccessDenied       = &Error{http.StatusForbidden, "access denied"}
	ErrAuthRequired       = &Error{http.StatusUnauthorized, "authentication required"}
	ErrResourceGone       = &Error{http.StatusGone, "resource gone"}
)

// ErrInvalidResponse is a general error for invalid server responses.
var ErrInvalidResponse = errors.New("invalid server response")

// ClassifyHTTPError converts an HTTP status code into an appropriate error.
// Returns predefined instances for specific errors, or new instances for others.
func ClassifyHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusNotFound:
		return ErrResourceNotFound
	case http.StatusForbidden:
		return ErrAccessDenied
	case http.StatusUnauthorized:
		return ErrAuthRequired
	case http.StatusGone:
		return ErrResourceGone
	case http.StatusMethodNotAllowed:
		return ErrHeadNotSupported
	case http.StatusRequestedRangeNotSatisfiable:
		return ErrRangesNotSupported
	default:
		// For server errors (5xx) and other client errors (4xx), create new instances.
		if statusCode >= 500 {
			return &Error{statusCode, fmt.Sprintf("server error (%d)", statusCode)}
		}
		if statusCode >= 400 {
			return &Error{statusCode, fmt.Sprintf("client error (%d)", statusCode)}
		}
		// For success codes (e.g., 200) or redirects, return nil unless otherwise needed.
		return nil
	}
}

// ClassifyError categorizes a general error into a custom error type.
// Focuses on network errors here, but can be extended for other cases.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	// Check for network-related errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return &NetworkError{NetworkErrorTimeout, "connection timed out", err}
		}
	}
	// Use string matching for cases where type assertion alone isn’t sufficient.
	// These are heuristics and may need refinement based on actual error types.
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return &NetworkError{NetworkErrorReset, "connection reset", err}
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such host") {
		return &NetworkError{NetworkErrorDNS, "DNS resolution failed", err}
	}
	if strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return &NetworkError{NetworkErrorTLS, "TLS/SSL error", err}
	}
	// If no specific classification applies, return the original error.
	return err
}

// IsRetryableError determines if an error is retryable.
// Includes certain network errors and server-side HTTP errors (5xx, except 501).
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Check network errors by kind.
	var netErr *NetworkError
	if errors.As(err, &netErr) {
		switch netErr.Kind {
		case NetworkErrorTimeout, NetworkErrorReset, NetworkErrorTLS:
			return true
		}
	}
	// Check HTTP errors for server-side issues.
	var httpErr *Error
	if errors.As(err, &httpErr) {
		// Retry 5xx errors except 501 (Not Implemented), as it’s typically not transient.
		return httpErr.StatusCode >= 500 && httpErr.StatusCode != http.StatusNotImplemented
	}
	return false
}

// IsFallbackError determines if an error triggers a fallback mechanism.
// Typically for errors where an alternative approach (e.g., different method) might work.
func IsFallbackError(err error) bool {
	return errors.Is(err, ErrHeadNotSupported) || errors.Is(err, ErrRangesNotSupported)
}

// IsFatalError determines if an error is fatal and processing should stop.
// Includes resource-related client errors (4xx) that indicate unresolvable issues.
func IsFatalError(err error) bool {
	return errors.Is(err, ErrResourceNotFound) ||
		errors.Is(err, ErrAccessDenied) ||
		errors.Is(err, ErrAuthRequired) ||
		errors.Is(err, ErrResourceGone)
}
