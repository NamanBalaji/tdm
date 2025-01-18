package http

import (
	"fmt"
)

type ErrorType int

const (
	ErrorTypeNetwork ErrorType = iota
	ErrorTypeHTTP
	ErrorTypeValidation
	ErrorTypeTimeout
)

type HTTPError struct {
	Type      ErrorType
	Operation string
	URL       string
	Status    int
	Err       error
}

func (e *HTTPError) Error() string {
	switch e.Type {
	case ErrorTypeHTTP:
		return fmt.Sprintf("HTTP error during %s for %s: status %d: %v",
			e.Operation, e.URL, e.Status, e.Err)
	case ErrorTypeNetwork:
		return fmt.Sprintf("network error during %s for %s: %v",
			e.Operation, e.URL, e.Err)
	case ErrorTypeTimeout:
		return fmt.Sprintf("timeout during %s for %s: %v",
			e.Operation, e.URL, e.Err)
	default:
		return fmt.Sprintf("error during %s for %s: %v",
			e.Operation, e.URL, e.Err)
	}
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}
