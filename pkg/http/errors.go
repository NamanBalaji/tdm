package http

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
)

var (
	ErrHeadNotSupported    = errors.New("HEAD method not supported by server")
	ErrRangesNotSupported  = errors.New("byte ranges not supported by server")
	ErrInvalidContentRange = errors.New("invalid Content-Range header")

	ErrTimeout         = errors.New("operation timed out")
	ErrNetworkProblem  = errors.New("network-related error")
	ErrIOProblem       = errors.New("I/O error")
	ErrRequestCreation = errors.New("failed to create request")

	ErrServerProblem    = errors.New("server error (5xx)")
	ErrTooManyRequests  = errors.New("too many requests (429)")
	ErrResourceNotFound = errors.New("resource not found (404)")
	ErrAccessDenied     = errors.New("access denied (403)")
	ErrAuthentication   = errors.New("authentication required (401)")
	ErrGone             = errors.New("resource gone (410)")
	ErrClientRequest    = errors.New("client error (4xx)")

	ErrUnknown       = errors.New("unknown error")
	ErrUnexpectedEOF = errors.New("unexpected EOF")
)

// ClassifyHTTPError converts an HTTP status code into an appropriate error.
func ClassifyHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusNotFound:
		return ErrResourceNotFound
	case http.StatusForbidden:
		return ErrAccessDenied
	case http.StatusUnauthorized:
		return ErrAuthentication
	case http.StatusGone:
		return ErrGone
	case http.StatusMethodNotAllowed:
		return ErrHeadNotSupported
	case http.StatusRequestedRangeNotSatisfiable:
		return ErrRangesNotSupported
	case http.StatusTooManyRequests:
		return ErrTooManyRequests
	default:
		switch {
		case statusCode >= http.StatusInternalServerError:
			return ErrServerProblem
		case statusCode >= http.StatusBadRequest:
			return ErrClientRequest
		default:
			return nil
		}
	}
}

// ClassifyError categorizes a general error into a sentinel error.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return err
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}

	if errors.Is(err, io.EOF) {
		return ErrUnexpectedEOF
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrUnexpectedEOF
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrNetworkProblem
	}

	return ErrUnknown
}

// IsFallbackError checks if the error requires fallback during initialization.
func IsFallbackError(err error) bool {
	return errors.Is(err, ErrHeadNotSupported) || errors.Is(err, ErrRangesNotSupported) || errors.Is(err, ErrUnexpectedEOF)
}
