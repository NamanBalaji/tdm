package http_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/NamanBalaji/tdm/internal/errors"
	httpprotocol "github.com/NamanBalaji/tdm/internal/protocol/http"
)

func TestClassifyHTTPError(t *testing.T) {
	testURL := "http://example.com/test"
	tests := []struct {
		status     int
		wantErrMsg string
		wantRetry  bool
	}{
		{http.StatusNotFound, "resource not found", false},
		{http.StatusForbidden, "access denied", false},
		{http.StatusUnauthorized, "authentication required", false},
		{http.StatusGone, "resource gone", false},
		{http.StatusMethodNotAllowed, "HEAD method not supported", false},
		{http.StatusRequestedRangeNotSatisfiable, "byte ranges not supported", false},
		{http.StatusTooManyRequests, "too many requests", true},
		{http.StatusInternalServerError, "server error (500)", true},
		{http.StatusServiceUnavailable, "server error (503)", true},
		{http.StatusBadRequest, "client error (400)", false},
		{http.StatusOK, "", false}, // No error for success codes
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Status%d", tt.status), func(t *testing.T) {
			err := httpprotocol.ClassifyHTTPError(tt.status, testURL)

			if tt.wantErrMsg == "" {
				if err != nil {
					t.Errorf("expected nil error for status %d, got %v", tt.status, err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error for status %d, got nil", tt.status)
			}

			var downloadErr *errors.DownloadError
			if !errors.As(err, &downloadErr) {
				t.Fatalf("expected *errors.DownloadError, got %T", err)
			}

			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("expected error to contain %q, got %q", tt.wantErrMsg, err.Error())
			}

			if downloadErr.Protocol != errors.ProtocolHTTP {
				t.Errorf("expected protocol HTTP, got %v", downloadErr.Protocol)
			}

			if downloadErr.Resource != testURL {
				t.Errorf("expected resource %q, got %q", testURL, downloadErr.Resource)
			}

			if downloadErr.StatusCode != tt.status {
				t.Errorf("expected status code %d, got %d", tt.status, downloadErr.StatusCode)
			}

			if downloadErr.Retryable != tt.wantRetry {
				t.Errorf("expected retryable=%v, got %v", tt.wantRetry, downloadErr.Retryable)
			}
		})
	}
}

// fakeNetError mimics the net.Error interface for testing
type fakeNetError struct {
	err     string
	timeout bool
}

func (f fakeNetError) Error() string   { return f.err }
func (f fakeNetError) Timeout() bool   { return f.timeout }
func (f fakeNetError) Temporary() bool { return f.timeout }

func TestClassifyError(t *testing.T) {
	testURL := "http://example.com/test"

	tests := []struct {
		name      string
		err       error
		wantCat   errors.ErrorCategory
		wantProto errors.Protocol
		wantRetry bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantCat:   "",
			wantProto: "",
			wantRetry: false,
		},
		{
			name:      "network timeout",
			err:       fakeNetError{"operation timed out", true},
			wantCat:   errors.CategoryNetwork,
			wantProto: errors.ProtocolGeneric,
			wantRetry: true,
		},
		{
			name:      "connection refused",
			err:       stderrors.New("connection refused by server"),
			wantCat:   errors.CategoryNetwork,
			wantProto: errors.ProtocolGeneric,
			wantRetry: true,
		},
		{
			name:      "dns resolution",
			err:       stderrors.New("no such host found"),
			wantCat:   errors.CategoryNetwork,
			wantProto: errors.ProtocolGeneric,
			wantRetry: false,
		},
		{
			name:      "tls error",
			err:       stderrors.New("TLS handshake failed"),
			wantCat:   errors.CategoryNetwork,
			wantProto: errors.ProtocolGeneric,
			wantRetry: true,
		},
		{
			name:      "context cancelled",
			err:       context.Canceled,
			wantCat:   errors.CategoryContext,
			wantProto: errors.ProtocolGeneric,
			wantRetry: false,
		},
		{
			name:      "context deadline",
			err:       context.DeadlineExceeded,
			wantCat:   errors.CategoryContext,
			wantProto: errors.ProtocolGeneric,
			wantRetry: false,
		},
		{
			name:      "unknown error",
			err:       stderrors.New("some other error"),
			wantCat:   errors.CategoryUnknown,
			wantProto: errors.ProtocolGeneric,
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := httpprotocol.ClassifyError(tt.err, testURL)

			if tt.err == nil {
				if result != nil {
					t.Errorf("expected nil result for nil error, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result, got nil")
			}

			var downloadErr *errors.DownloadError
			if !errors.As(result, &downloadErr) {
				t.Fatalf("expected *errors.DownloadError, got %T", result)
			}

			if downloadErr.Category != tt.wantCat {
				t.Errorf("expected category %v, got %v", tt.wantCat, downloadErr.Category)
			}

			if downloadErr.Protocol != tt.wantProto {
				t.Errorf("expected protocol %v, got %v", tt.wantProto, downloadErr.Protocol)
			}

			if downloadErr.Retryable != tt.wantRetry {
				t.Errorf("expected retryable=%v, got %v", tt.wantRetry, downloadErr.Retryable)
			}

			if downloadErr.Resource != testURL {
				t.Errorf("expected resource %q, got %q", testURL, downloadErr.Resource)
			}
		})
	}
}

func TestIsFallbackError(t *testing.T) {
	headErr := errors.NewHTTPError(httpprotocol.ErrHeadNotSupported, "http://example.com", http.StatusMethodNotAllowed)
	rangeErr := errors.NewHTTPError(httpprotocol.ErrRangesNotSupported, "http://example.com", http.StatusRequestedRangeNotSatisfiable)
	otherErr := errors.NewHTTPError(errors.ErrResourceNotFound, "http://example.com", http.StatusNotFound)

	if !httpprotocol.IsFallbackError(headErr) {
		t.Error("expected ErrHeadNotSupported to be a fallback error")
	}

	if !httpprotocol.IsFallbackError(rangeErr) {
		t.Error("expected ErrRangesNotSupported to be a fallback error")
	}

	if httpprotocol.IsFallbackError(otherErr) {
		t.Error("expected resource not found to not be a fallback error")
	}

	if httpprotocol.IsFallbackError(stderrors.New("random error")) {
		t.Error("expected random error to not be a fallback error")
	}
}

func TestErrorWrapping(t *testing.T) {
	baseErr := httpprotocol.ErrHeadNotSupported
	wrappedErr := errors.NewHTTPError(baseErr, "http://example.com", http.StatusMethodNotAllowed)

	if !errors.Is(wrappedErr, httpprotocol.ErrHeadNotSupported) {
		t.Error("errors.Is should find the wrapped error")
	}

	var downloadErr *errors.DownloadError
	if !errors.As(wrappedErr, &downloadErr) {
		t.Error("errors.As should unwrap to DownloadError")
	}

	if downloadErr.Protocol != errors.ProtocolHTTP {
		t.Errorf("expected protocol HTTP, got %v", downloadErr.Protocol)
	}
}
