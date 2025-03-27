package errors_test

import (
	stdErrors "errors"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/errors"
)

func TestDownloadErrorError(t *testing.T) {
	baseErr := stdErrors.New("underlying error")
	de := &errors.DownloadError{
		Err:       baseErr,
		Category:  errors.CategoryIO,
		Protocol:  errors.ProtocolGeneric,
		Retryable: false,
		Timestamp: time.Now(),
		Resource:  "file.txt",
	}
	expected := "[IO] file.txt: underlying error"
	if de.Error() != expected {
		t.Errorf("expected %q, got %q", expected, de.Error())
	}

	de2 := &errors.DownloadError{
		Err:        stdErrors.New("server error"),
		Category:   errors.CategoryProtocol,
		Protocol:   errors.ProtocolHTTP,
		Retryable:  true,
		Timestamp:  time.Now(),
		Resource:   "http://example.com",
		StatusCode: 500,
	}
	expected2 := "[HTTP:PROTOCOL] http://example.com (status: 500): server error"
	if de2.Error() != expected2 {
		t.Errorf("expected %q, got %q", expected2, de2.Error())
	}
}

func TestDownloadErrorUnwrap(t *testing.T) {
	baseErr := stdErrors.New("base error")
	de := &errors.DownloadError{
		Err:       baseErr,
		Category:  errors.CategoryNetwork,
		Protocol:  errors.ProtocolGeneric,
		Retryable: true,
		Timestamp: time.Now(),
		Resource:  "resource",
	}
	if !errors.Is(baseErr, stdErrors.Unwrap(de)) {
		t.Errorf("expected underlying error %v, got %v", baseErr, stdErrors.Unwrap(de))
	}
}

func TestNewNetworkError(t *testing.T) {
	baseErr := stdErrors.New("connection error")
	de := errors.NewNetworkError(baseErr, "example.com", true)
	if !errors.Is(baseErr, de.Err) || de.Category != errors.CategoryNetwork || de.Protocol != errors.ProtocolGeneric ||
		de.Retryable != true || de.Resource != "example.com" {
		t.Error("NewNetworkError did not set fields correctly")
	}
	if de.Timestamp.IsZero() {
		t.Error("Timestamp not set in NewNetworkError")
	}
}

func TestNewIOError(t *testing.T) {
	baseErr := stdErrors.New("io error")
	de := errors.NewIOError(baseErr, "file.txt")
	if !errors.Is(baseErr, de.Err) || de.Category != errors.CategoryIO || de.Protocol != errors.ProtocolGeneric ||
		de.Retryable != false || de.Resource != "file.txt" {
		t.Error("NewIOError did not set fields correctly")
	}
}

func TestNewContextError(t *testing.T) {
	baseErr := stdErrors.New("context canceled")
	de := errors.NewContextError(baseErr, "operation")
	if !errors.Is(baseErr, de.Err) || de.Category != errors.CategoryContext || de.Protocol != errors.ProtocolGeneric ||
		de.Retryable != false || de.Resource != "operation" {
		t.Error("NewContextError did not set fields correctly")
	}
}

func TestNewHTTPError(t *testing.T) {
	baseErr := stdErrors.New("server error")
	de := errors.NewHTTPError(baseErr, "http://example.com", 500)
	if de.Protocol != errors.ProtocolHTTP {
		t.Error("Expected ProtocolHTTP")
	}
	if de.Category != errors.CategoryProtocol {
		t.Error("Expected CategoryProtocol")
	}
	if !de.Retryable {
		t.Error("Expected retryable true for status 500")
	}
	if de.StatusCode != 500 {
		t.Error("StatusCode mismatch for status 500")
	}

	de2 := errors.NewHTTPError(stdErrors.New("too many requests"), "http://example.com", 429)
	if !de2.Retryable {
		t.Error("Expected retryable true for status 429")
	}

	de3 := errors.NewHTTPError(stdErrors.New("bad request"), "http://example.com", 400)
	if de3.Category != errors.CategoryResource {
		t.Errorf("Expected CategoryResource for status 400, got %s", de3.Category)
	}
}

func TestIsRetryable(t *testing.T) {
	de := errors.NewNetworkError(stdErrors.New("error"), "example.com", true)
	if !errors.IsRetryable(de) {
		t.Error("Expected retryable error to be retried")
	}

	de2 := errors.NewIOError(stdErrors.New("io error"), "file.txt")
	if errors.IsRetryable(de2) {
		t.Error("Expected non-retryable error to not be retried")
	}

	if errors.IsRetryable(nil) {
		t.Error("Expected nil error to be non-retryable")
	}
}

func TestIsNetworkError(t *testing.T) {
	de := errors.NewNetworkError(stdErrors.New("net error"), "example.com", true)
	if !errors.IsNetworkError(de) {
		t.Error("Expected error to be identified as network error")
	}

	de2 := errors.NewIOError(stdErrors.New("io error"), "file.txt")
	if errors.IsNetworkError(de2) {
		t.Error("Expected non-network error to not be identified as network error")
	}
}

func TestIsIOError(t *testing.T) {
	de := errors.NewIOError(stdErrors.New("io error"), "file.txt")
	if !errors.IsIOError(de) {
		t.Error("Expected error to be identified as I/O error")
	}

	de2 := errors.NewNetworkError(stdErrors.New("net error"), "example.com", true)
	if errors.IsIOError(de2) {
		t.Error("Expected non-I/O error to not be identified as I/O error")
	}
}

func TestIsProtocolError(t *testing.T) {
	de := errors.NewHTTPError(stdErrors.New("server error"), "http://example.com", 500)
	if !errors.IsProtocolError(de, errors.ProtocolHTTP) {
		t.Error("Expected error to be identified as HTTP protocol error")
	}
	if errors.IsProtocolError(de, errors.ProtocolGeneric) {
		t.Error("Did not expect error to be identified as Generic protocol error")
	}
}

func TestGetStatusCode(t *testing.T) {
	de := errors.NewHTTPError(stdErrors.New("server error"), "http://example.com", 500)
	code, ok := errors.GetStatusCode(de)
	if !ok {
		t.Error("Expected status code to be available")
	}
	if code != 500 {
		t.Errorf("Expected status code 500, got %d", code)
	}

	otherErr := stdErrors.New("other error")
	if _, ok := errors.GetStatusCode(otherErr); ok {
		t.Error("Expected no status code for a non-DownloadError")
	}
}

func TestWithDetails(t *testing.T) {
	de := errors.NewNetworkError(stdErrors.New("net error"), "example.com", true)
	details := map[string]interface{}{
		"key1": "value1",
		"key2": 2,
	}
	errWithDetails := errors.WithDetails(de, details)
	if !errors.Is(errWithDetails, de) {
		t.Error("WithDetails should return the original error instance")
	}
	for k, v := range details {
		if de.Details[k] != v {
			t.Errorf("expected de.Details[%q] = %v, got %v", k, v, de.Details[k])
		}
	}

	otherErr := stdErrors.New("not a DownloadError")
	if !errors.Is(otherErr, errors.WithDetails(otherErr, details)) {
		t.Error("WithDetails should return the original error when not a DownloadError")
	}
}
