package connection

import (
	"context"
	"time"
)

// Connection represents a network connection to a remote resource.
type Connection interface {
	// Connect establishes the connection
	Connect(ctx context.Context) error
	// Read reads data from the connection into the buffer
	Read(ctx context.Context, p []byte) (n int, err error)
	// Close closes the connection
	Close() error
	// IsAlive checks if the connection is still active
	IsAlive() bool
	// Reset reestablishes a dropped connection
	Reset(ctx context.Context) error
	// GetURL returns the connection's URL (for connection pooling)
	GetURL() string
	// GetHeaders returns the connection's headers (for connection pooling)
	GetHeaders() map[string]string
	// SetHeader sets a specific header value
	SetHeader(key, value string)
	// SetTimeout sets read/write timeouts
	SetTimeout(timeout time.Duration)
}
