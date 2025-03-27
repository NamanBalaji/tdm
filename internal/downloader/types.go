package downloader

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/google/uuid"
)

// Config contains all download configuration options
type Config struct {
	// File and location options
	Directory string `json:"directory"` // Target directory

	// Connection options
	Connections int               `json:"connections"`       // Number of parallel connections
	Headers     map[string]string `json:"headers,omitempty"` // Custom headers

	// Retry behavior
	MaxRetries int           `json:"max_retries"`           // Maximum number of retries
	RetryDelay time.Duration `json:"retry_delay,omitempty"` // Delay between retries

	// Performance options
	ThrottleSpeed      int64 `json:"throttle_speed,omitempty"`      // Bandwidth throttle in bytes/sec
	DisableParallelism bool  `json:"disable_parallelism,omitempty"` // Force single connection

	// Priority
	Priority int `json:"priority"` // Priority level (higher = more important)

	// Verification
	Checksum          string `json:"checksum,omitempty"`           // File checksum
	ChecksumAlgorithm string `json:"checksum_algorithm,omitempty"` // Checksum algorithm

	// Resume behavior
	UseExistingFile bool `json:"use_existing_file,omitempty"` // Resume from existing file
}

// SpeedCalculator handles download speed measurement
type SpeedCalculator struct {
	samples        []int64   // Recent speed samples
	lastCheck      time.Time // Time of last measurement
	bytesSinceLast int64     // Bytes downloaded since last check
	windowSize     int       // Number of samples to keep
	mu             sync.Mutex
}

// NewSpeedCalculator creates a new speed calculator
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

// AddBytes records additional downloaded bytes
func (sc *SpeedCalculator) AddBytes(bytes int64) {
	atomic.AddInt64(&sc.bytesSinceLast, bytes)
}

// GetSpeed calculates current download speed in bytes/sec
func (sc *SpeedCalculator) GetSpeed() int64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(sc.lastCheck)

	// Only recalculate after a reasonable interval
	if elapsed < time.Second {
		if len(sc.samples) > 0 {
			return sc.samples[len(sc.samples)-1]
		}
		return 0
	}

	// Calculate bytes per second
	bytesSinceLast := atomic.SwapInt64(&sc.bytesSinceLast, 0)
	speed := int64(float64(bytesSinceLast) / elapsed.Seconds())

	// Update samples (keep last N samples)
	sc.samples = append(sc.samples, speed)
	if len(sc.samples) > sc.windowSize {
		sc.samples = sc.samples[1:]
	}
	sc.lastCheck = now

	// Return average speed for smoother readings
	return sc.getAverageSpeed()
}

// getAverageSpeed calculates average of recent speed samples
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

// Stats represents current download statistics
type Stats struct {
	ID              uuid.UUID
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
