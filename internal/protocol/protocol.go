package protocol

import (
	"errors"
	"github.com/NamanBalaji/tdm/internal/common"
	"sync"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/protocol/http"
)

// Protocol defines the interface for handling different protocols
type Protocol interface {
	// CanHandle checks if this handler can handle the given URL
	CanHandle(url string) bool
	// Initialize gathers information about the download resource
	Initialize(url string, config *downloader.Config) (*common.DownloadInfo, error)
	// CreateConnection creates a new connection for chunk download
	CreateConnection(urlStr string, chunk *chunk.Chunk, downloadConfig *downloader.Config) (connection.Connection, error)
}

var (
	// ErrUnsupportedProtocol is returned when no handler can handle the URL
	ErrUnsupportedProtocol = errors.New("unsupported protocol")

	// ErrInvalidURL is returned for malformed URLs
	ErrInvalidURL = errors.New("invalid URL")
)

type Handler struct {
	mu        sync.RWMutex
	protocols []Protocol
}

func NewHandler() *Handler {
	return &Handler{
		protocols: []Protocol{
			http.NewHandler(),
		},
	}
}

// RegisterProtocol adds a protocol to the handler
func (h *Handler) RegisterProtocol(p Protocol) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.protocols = append(h.protocols, p)
}

// Initialize initializes a download by finding the appropriate handler and gathering information
func (h *Handler) Initialize(url string, config *downloader.Config) (*common.DownloadInfo, error) {
	handler, err := h.getProtocolHandler(url)
	if err != nil {
		return nil, err
	}

	return handler.Initialize(url, config)
}

func (h *Handler) getProtocolHandler(url string) (Protocol, error) {
	if url == "" {
		return nil, ErrInvalidURL
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.protocols {
		if handler.CanHandle(url) {
			return handler, nil
		}
	}

	return nil, ErrUnsupportedProtocol
}

// GetHandler returns a handler that can handle the given URL
func (h *Handler) GetHandler(url string) (Protocol, error) {
	return h.getProtocolHandler(url)
}
