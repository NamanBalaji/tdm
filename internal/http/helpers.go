package http

import (
	"math/rand"
	"time"

	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

var retryableErrors = map[error]struct{}{
	httpPkg.ErrNetworkProblem:  {},
	httpPkg.ErrServerProblem:   {},
	httpPkg.ErrTooManyRequests: {},
	httpPkg.ErrTimeout:         {},
	ErrChunkFileWriteFailed:    {},
}

func isRetryableError(err error) bool {
	_, ok := retryableErrors[err]
	return ok
}

func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s...
	delay := baseDelay * (1 << uint(retryCount))

	// Add jitter to prevent thundering herd problem
	jitter := time.Duration(rand.Float64() * float64(delay) * 0.2) // +/- 10%
	finalDelay := delay + jitter - (time.Duration(float64(delay) * 0.1))

	maxDelay := 2 * time.Minute
	if finalDelay > maxDelay {
		finalDelay = maxDelay
	}

	return finalDelay
}

func CanHandle(urlStr string) bool {
	return httpPkg.IsDownloadable(urlStr)
}
