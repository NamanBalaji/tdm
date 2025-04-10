package engine

import (
	"math/rand"
	"time"

	"github.com/NamanBalaji/tdm/internal/protocol/http"
)

var retryableErrors = map[error]struct{}{
	http.ErrNetworkProblem:  {},
	http.ErrServerProblem:   {},
	http.ErrTooManyRequests: {},
	http.ErrTimeout:         {},
}

// calculateBackoff calculates a backoff duration with jitter.
func calculateBackoff(retryCount int, baseDelay time.Duration) time.Duration {
	// Exponential backoff: 2^retryCount * baseDelay
	delay := baseDelay * (1 << uint(retryCount))

	// Apply jitter to avoid thundering herd (between 75% and 125% of computed delay)
	jitterFactor := 0.75 + 0.5*rand.Float64()
	jitter := time.Duration(float64(delay) * jitterFactor)

	// Cap maximum delay at 2 minutes
	maxDelay := 2 * time.Minute
	if jitter > maxDelay {
		jitter = maxDelay
	}

	return jitter
}

// isRetryableError checks if the error is in the retryable.
func isRetryableError(err error) bool {
	_, ok := retryableErrors[err]
	return ok
}
