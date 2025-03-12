package http

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestError_ErrorMethod(t *testing.T) {
	errInstance := &Error{
		StatusCode: 404,
		Message:    "not found",
	}
	expected := "HTTP error 404: not found"
	if errInstance.Error() != expected {
		t.Errorf("expected %q, got %q", expected, errInstance.Error())
	}
}

func TestNetworkError(t *testing.T) {
	cause := errors.New("underlying network failure")
	netErr := &NetworkError{
		Kind:    NetworkErrorTimeout,
		Message: "connection timed out",
		Cause:   cause,
	}
	if netErr.Error() != "connection timed out" {
		t.Errorf("expected message %q, got %q", "connection timed out", netErr.Error())
	}
	if !errors.Is(netErr, cause) {
		t.Errorf("expected unwrapped error to be %v", cause)
	}
}

type fakeNetError struct {
	err     string
	timeout bool
}

func (f fakeNetError) Error() string   { return f.err }
func (f fakeNetError) Timeout() bool   { return f.timeout }
func (f fakeNetError) Temporary() bool { return f.timeout }

func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		status   int
		expected error
	}{
		{http.StatusNotFound, ErrResourceNotFound},
		{http.StatusForbidden, ErrAccessDenied},
		{http.StatusUnauthorized, ErrAuthRequired},
		{http.StatusGone, ErrResourceGone},
		{http.StatusMethodNotAllowed, ErrHeadNotSupported},
		{http.StatusRequestedRangeNotSatisfiable, ErrRangesNotSupported},
		{402, nil},
		{502, nil},
		{200, nil},
	}

	for _, tt := range tests {
		err := ClassifyHTTPError(tt.status)
		if tt.expected != nil {
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected error %v for status %d, got %v", tt.expected, tt.status, err)
			}
		} else {
			if tt.status >= 400 && err == nil {
				t.Errorf("expected a non-nil error for status %d", tt.status)
			}
			if tt.status < 400 && err != nil {
				t.Errorf("expected nil error for status %d, got %v", tt.status, err)
			}
		}
	}
}

// TestClassifyError tests the ClassifyError function.
func TestClassifyError(t *testing.T) {
	if got := ClassifyError(nil); got != nil {
		t.Errorf("expected nil error, got %v", got)
	}

	timeoutErr := fakeNetError{"operation timed out", true}
	classified := ClassifyError(timeoutErr)
	var netErr *NetworkError
	if !errors.As(classified, &netErr) || netErr.Kind != NetworkErrorTimeout {
		t.Errorf("expected NetworkError with kind %q, got %v", NetworkErrorTimeout, classified)
	}

	connRefused := errors.New("connection refused by server")
	classified = ClassifyError(connRefused)
	if !errors.As(classified, &netErr) || netErr.Kind != NetworkErrorReset {
		t.Errorf("expected NetworkError with kind %q, got %v", NetworkErrorReset, classified)
	}

	noHost := errors.New("no such host found")
	classified = ClassifyError(noHost)
	if !errors.As(classified, &netErr) || netErr.Kind != NetworkErrorDNS {
		t.Errorf("expected NetworkError with kind %q, got %v", NetworkErrorDNS, classified)
	}

	tlsErr := errors.New("TLS handshake failed due to certificate error")
	classified = ClassifyError(tlsErr)
	if !errors.As(classified, &netErr) || netErr.Kind != NetworkErrorTLS {
		t.Errorf("expected NetworkError with kind %q, got %v", NetworkErrorTLS, classified)
	}

	otherErr := errors.New("some other error")
	classified = ClassifyError(otherErr)
	if !errors.Is(otherErr, classified) {
		t.Errorf("expected original error %v, got %v", otherErr, classified)
	}
}

// TestIsRetryableError tests the IsRetryableError function.
func TestIsRetryableError(t *testing.T) {
	retryableNetErrors := []*NetworkError{
		{Kind: NetworkErrorTimeout, Message: "timeout"},
		{Kind: NetworkErrorReset, Message: "reset"},
		{Kind: NetworkErrorTLS, Message: "tls error"},
	}
	for _, err := range retryableNetErrors {
		if !IsRetryableError(err) {
			t.Errorf("expected error %v to be retryable", err)
		}
	}

	dnsErr := &NetworkError{Kind: NetworkErrorDNS, Message: "DNS error"}
	if IsRetryableError(dnsErr) {
		t.Errorf("expected error %v not to be retryable", dnsErr)
	}

	httpErr := &Error{StatusCode: 502, Message: fmt.Sprintf("server error (%d)", 502)}
	if !IsRetryableError(httpErr) {
		t.Errorf("expected HTTP error %v to be retryable", httpErr)
	}

	httpErr = &Error{StatusCode: 501, Message: fmt.Sprintf("server error (%d)", 501)}
	if IsRetryableError(httpErr) {
		t.Errorf("expected HTTP error %v not to be retryable", httpErr)
	}

	if IsRetryableError(nil) {
		t.Error("expected nil error not to be retryable")
	}
}

func TestIsFallbackError(t *testing.T) {
	if !IsFallbackError(ErrHeadNotSupported) {
		t.Errorf("expected ErrHeadNotSupported to be a fallback error")
	}
	if !IsFallbackError(ErrRangesNotSupported) {
		t.Errorf("expected ErrRangesNotSupported to be a fallback error")
	}
	if IsFallbackError(ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied not to be a fallback error")
	}
}

func TestIsFatalError(t *testing.T) {
	fatalErrors := []error{
		ErrResourceNotFound,
		ErrAccessDenied,
		ErrAuthRequired,
		ErrResourceGone,
	}
	for _, err := range fatalErrors {
		if !IsFatalError(err) {
			t.Errorf("expected error %v to be fatal", err)
		}
	}
	nonFatal := errors.New("non-fatal error")
	if IsFatalError(nonFatal) {
		t.Errorf("expected non-fatal error %v not to be fatal", nonFatal)
	}
}
