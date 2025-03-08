package connection

import "time"

// Connection represents a network connection to a remote resource
type Connection interface {
	// Connect establishes the connection
	Connect() error
	// Read reads data from the connection into the buffer
	Read(p []byte) (n int, err error)
	// Close closes the connection
	Close() error
	// IsAlive checks if the connection is still active
	IsAlive() bool
	// Reset reestablishes a dropped connection
	Reset() error
	// SetTimeout sets read/write timeouts
	SetTimeout(timeout time.Duration)
}
