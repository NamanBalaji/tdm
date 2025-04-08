package engine

import (
	"math/rand"
	"time"
)

// calculateBackoff calculates a backoff duration with jitter
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
