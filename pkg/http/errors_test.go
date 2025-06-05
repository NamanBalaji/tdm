package http_test

import (
	"context"
	"errors"
	"io"
	"testing"

	httpmod "github.com/NamanBalaji/tdm/pkg/http"
)

// fakeNetErr simulates a net.Error to test ClassifyError behavior.
type fakeNetErr struct{}

func (f *fakeNetErr) Error() string   { return "simulated network error" }
func (f *fakeNetErr) Timeout() bool   { return false }
func (f *fakeNetErr) Temporary() bool { return false }

func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"NotFound 404", 404, httpmod.ErrResourceNotFound},
		{"Forbidden 403", 403, httpmod.ErrAccessDenied},
		{"Unauthorized 401", 401, httpmod.ErrAuthentication},
		{"Gone 410", 410, httpmod.ErrGone},
		{"MethodNotAllowed 405", 405, httpmod.ErrHeadNotSupported},
		{"RangeNotSatisfiable 416", 416, httpmod.ErrRangesNotSupported},
		{"TooManyRequests 429", 429, httpmod.ErrTooManyRequests},
		{"ServerError 500", 500, httpmod.ErrServerProblem},
		{"ServerError 503", 503, httpmod.ErrServerProblem},
		{"ClientError 450", 450, httpmod.ErrClientRequest},
		{"ClientError 400", 400, httpmod.ErrClientRequest},
		{"OK 200", 200, nil},
		{"Informational 100", 100, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.ClassifyHTTPError(tt.statusCode)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("ClassifyHTTPError(%d) = %v; want %v", tt.statusCode, got, tt.wantErr)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		input   error
		wantErr error
	}{
		{"Nil error", nil, nil},
		{"ContextCanceled", context.Canceled, context.Canceled},
		{"DeadlineExceeded", context.DeadlineExceeded, httpmod.ErrTimeout},
		{"EOF", io.EOF, httpmod.ErrUnexpectedEOF},
		{"UnexpectedEOF", io.ErrUnexpectedEOF, httpmod.ErrUnexpectedEOF},
		{"NetError", &fakeNetErr{}, httpmod.ErrNetworkProblem},
		{"Other error", errors.New("some random error"), httpmod.ErrUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.ClassifyError(tt.input)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("ClassifyError(%v) = %v; want %v", tt.input, got, tt.wantErr)
			}
		})
	}
}

func TestIsFallbackError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"HeadNotSupported", httpmod.ErrHeadNotSupported, true},
		{"RangesNotSupported", httpmod.ErrRangesNotSupported, true},
		{"UnexpectedEOF", httpmod.ErrUnexpectedEOF, true},
		{"TimeoutNotFallback", httpmod.ErrTimeout, false},
		{"IOProblemNotFallback", httpmod.ErrIOProblem, false},
		{"NilNotFallback", nil, false},
		{"OtherErrorNotFallback", errors.New("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpmod.IsFallbackError(tt.err)
			if got != tt.want {
				t.Errorf("IsFallbackError(%v) = %v; want %v", tt.err, got, tt.want)
			}
		})
	}
}
