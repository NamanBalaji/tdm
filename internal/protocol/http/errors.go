package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/NamanBalaji/tdm/internal/errors"
)

// Common HTTP error constants
var (
	ErrHeadNotSupported    = errors.New("HEAD method not supported by server")
	ErrRangesNotSupported  = errors.New("byte ranges not supported by server")
	ErrInvalidContentRange = errors.New("invalid content range")
)

// ClassifyHTTPError converts an HTTP status code into an appropriate error
func ClassifyHTTPError(statusCode int, url string) error {
	var baseErr error

	switch statusCode {
	case http.StatusNotFound:
		baseErr = errors.ErrResourceNotFound
	case http.StatusForbidden:
		baseErr = errors.ErrAccessDenied
	case http.StatusUnauthorized:
		baseErr = errors.ErrAuthentication
	case http.StatusGone:
		baseErr = errors.New("resource gone")
	case http.StatusMethodNotAllowed:
		baseErr = ErrHeadNotSupported
	case http.StatusRequestedRangeNotSatisfiable:
		baseErr = ErrRangesNotSupported
	case http.StatusTooManyRequests:
		baseErr = errors.New("too many requests")
	default:
		switch {
		case statusCode >= 500:
			message := fmt.Sprintf("server error (%d)", statusCode)
			baseErr = errors.New(message)
		case statusCode >= 400:
			message := fmt.Sprintf("client error (%d)", statusCode)
			baseErr = errors.New(message)
		default:
			return nil
		}
	}

	return errors.NewHTTPError(baseErr, url, statusCode)
}

// ClassifyError categorizes a general error into a custom error type
func ClassifyError(err error, url string) error {
	if err == nil {
		return nil
	}

	var downloadErr *errors.DownloadError
	if errors.As(err, &downloadErr) {
		return err
	}

	if errors.Is(err, context.Canceled) {
		return errors.NewContextError(err, url)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return errors.NewContextError(err, url)
	}

	if errors.Is(err, io.EOF) {
		return errors.NewHTTPError(errors.ErrEndOfFIle, url, http.StatusMethodNotAllowed)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return errors.NewNetworkError(errors.ErrTimeout, url, true)
		}
		return errors.NewNetworkError(err, url, true)
	}

	errStr := strings.ToLower(err.Error())

	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") {
		return errors.NewNetworkError(errors.ErrConnectionReset, url, true)
	}

	if strings.Contains(errStr, "no such host") {
		return errors.NewNetworkError(errors.New("DNS resolution failed"), url, false)
	}

	if strings.Contains(errStr, "tls") ||
		strings.Contains(errStr, "certificate") {
		return errors.NewNetworkError(errors.New("TLS/SSL error"), url, true)
	}

	if strings.Contains(errStr, "context") &&
		(strings.Contains(errStr, "canceled") ||
			strings.Contains(errStr, "deadline exceeded")) {
		return errors.NewContextError(err, url)
	}

	return &errors.DownloadError{
		Err:       err,
		Category:  errors.CategoryUnknown,
		Protocol:  errors.ProtocolGeneric,
		Retryable: false,
		Resource:  url,
	}
}

// IsFallbackError determines if an error triggers a fallback mechanism
func IsFallbackError(err error) bool {
	return errors.Is(err, ErrHeadNotSupported) || errors.Is(err, ErrRangesNotSupported) || errors.Is(err, errors.ErrEndOfFIle)
}
