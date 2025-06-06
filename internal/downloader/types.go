package downloader

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/common"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

// SpeedCalculator handles download speed measurement.
type SpeedCalculator struct {
	samples        []int64
	lastCheck      time.Time
	bytesSinceLast int64
	windowSize     int
	mu             sync.Mutex
}

// NewSpeedCalculator creates a new speed calculator.
func NewSpeedCalculator(windowSize int) *SpeedCalculator {
	if windowSize <= 0 {
		windowSize = 5
	}
	return &SpeedCalculator{
		samples:        make([]int64, 0, windowSize),
		lastCheck:      time.Now(),
		bytesSinceLast: 0,
		windowSize:     windowSize,
	}
}

// AddBytes records additional downloaded bytes.
func (sc *SpeedCalculator) AddBytes(bytes int64) {
	atomic.AddInt64(&sc.bytesSinceLast, bytes)
}

// GetSpeed calculates the current download speed in bytes/sec.
func (sc *SpeedCalculator) GetSpeed() int64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(sc.lastCheck)

	if elapsed < time.Second {
		if len(sc.samples) > 0 {
			return sc.samples[len(sc.samples)-1]
		}
		return 0
	}

	bytesSinceLast := atomic.SwapInt64(&sc.bytesSinceLast, 0)
	speed := int64(float64(bytesSinceLast) / elapsed.Seconds())

	sc.samples = append(sc.samples, speed)
	if len(sc.samples) > sc.windowSize {
		sc.samples = sc.samples[1:]
	}
	sc.lastCheck = now

	return sc.getAverageSpeed()
}

// getAverageSpeed calculates the average of recent speed samples.
func (sc *SpeedCalculator) getAverageSpeed() int64 {
	if len(sc.samples) == 0 {
		return 0
	}

	var sum int64
	for _, speed := range sc.samples {
		sum += speed
	}

	return sum / int64(len(sc.samples))
}

// Stats represent current download statistics.
type Stats struct {
	ID              uuid.UUID
	Filename        string
	Status          common.Status
	TotalSize       int64
	Downloaded      int64
	Progress        float64
	Speed           int64
	TimeElapsed     time.Duration
	TimeRemaining   time.Duration
	ActiveChunks    int
	CompletedChunks int
	TotalChunks     int
	Error           string
	LastUpdated     time.Time
}

var retryableErrors = map[error]struct{}{
	httpPkg.ErrNetworkProblem:  {},
	httpPkg.ErrServerProblem:   {},
	httpPkg.ErrTooManyRequests: {},
	httpPkg.ErrTimeout:         {},
}

// isRetryableError checks if the error is in the retryable.
func isRetryableError(err error) bool {
	_, ok := retryableErrors[err]
	return ok
}
